package provider

import (
	"context"
	"crypto/rand"
	"fmt"
	"unicode/utf16"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	terrakube "github.com/terrakube-io/terrakube-go"
)

// roleToState converts a nullable API role string to a Terraform string value.
// nil or empty string becomes null — both represent "unset/custom" on the server.
func roleToState(r *string) types.String {
	if r == nil || *r == "" {
		return types.StringNull()
	}
	return types.StringValue(*r)
}

// accessPermissions are the attributes that project access and workspace access share.
type accessPermissions struct {
	Name            types.String `tfsdk:"name"`
	ManageState     types.Bool   `tfsdk:"manage_state"`
	ManageWorkspace types.Bool   `tfsdk:"manage_workspace"`
	ManageJob       types.Bool   `tfsdk:"manage_job"`
	PlanJob         types.Bool   `tfsdk:"plan_job"`
	ApproveJob      types.Bool   `tfsdk:"approve_job"`
	DestroyJob      types.Bool   `tfsdk:"destroy_job"`
	Role            types.String `tfsdk:"role"`
}

// attributes builds the attributes sent on create and on update. The permission flags are always
// sent, plan_job and approve_job inheriting manage_job when unset. destroy_job and role are sent
// only when known, never as null: older APIs reject destroy_job.
func (p accessPermissions) attributes() patchAttributes {
	attributes := patchAttributes{
		"manageState":     p.ManageState.ValueBool(),
		"manageWorkspace": p.ManageWorkspace.ValueBool(),
		"manageJob":       p.ManageJob.ValueBool(),
		"planJob":         resolveJobFlag(p.PlanJob, p.ManageJob),
		"approveJob":      resolveJobFlag(p.ApproveJob, p.ManageJob),
	}
	attributes.createString("name", p.Name)
	attributes.createBool("destroyJob", p.DestroyJob)
	attributes.createString("role", p.Role)
	return attributes
}

// setPermissions copies the permissions the API returned. The name is left as it is.
func (p *accessPermissions) setPermissions(manageState, manageWorkspace, manageJob, planJob, approveJob bool, destroyJob *bool, role string) {
	p.ManageState = types.BoolValue(manageState)
	p.ManageWorkspace = types.BoolValue(manageWorkspace)
	p.ManageJob = types.BoolValue(manageJob)
	p.PlanJob = types.BoolValue(planJob)
	p.ApproveJob = types.BoolValue(approveJob)
	p.DestroyJob = types.BoolPointerValue(destroyJob)
	p.Role = roleToState(&role)
}

// resolveJobFlag returns the explicit value when set; otherwise inherits from
// the fallback (used to inherit plan_job/approve_job from manage_job).
func resolveJobFlag(explicit, inherit types.Bool) bool {
	if !explicit.IsNull() && !explicit.IsUnknown() {
		return explicit.ValueBool()
	}
	return inherit.ValueBool()
}

// rbacRoleConflictValidator rejects configs where plan_job, approve_job, or destroy_job are
// explicitly set alongside a non-custom role. For non-custom roles the server
// ignores boolean flags, so setting them produces a contradictory config.
type rbacRoleConflictValidator struct{}

func (v rbacRoleConflictValidator) Description(_ context.Context) string {
	return "Rejects configs where plan_job/approve_job/destroy_job are set alongside a non-custom role"
}

func (v rbacRoleConflictValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v rbacRoleConflictValidator) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var role types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("role"), &role)...)
	if resp.Diagnostics.HasError() || role.IsNull() || role.IsUnknown() || role.ValueString() == "custom" {
		return
	}

	for _, attr := range []string{"plan_job", "approve_job", "destroy_job"} {
		var flag types.Bool
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(attr), &flag)...)
		if !flag.IsNull() && !flag.IsUnknown() {
			resp.Diagnostics.AddError(
				"Conflicting RBAC configuration",
				fmt.Sprintf("%s is set but role %q controls this permission — boolean flags are only used when role is \"custom\" or unset. Remove %s or set role = \"custom\".", attr, role.ValueString(), attr),
			)
		}
	}
}

// softDeletedName leaves room for the deletion suffix within API name limits.
func softDeletedName(name, randomSuffix string, maxLength int) string {
	suffix := "_DEL_" + randomSuffix
	length := 0
	for index, character := range name {
		length += utf16.RuneLen(character)
		if length > maxLength-len(suffix) {
			return name[:index] + suffix
		}
	}
	return name + suffix
}

// randomSoftDeletedName renames a soft-deleted object the way the Terrakube UI does: softDeletedName
// with a random four-character suffix, so the original name can be used again.
func randomSoftDeletedName(name string, maxLength int) (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890"
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return "", err
	}
	for i := range suffix {
		suffix[i] = chars[int(suffix[i])%len(chars)]
	}
	return softDeletedName(name, string(suffix), maxLength), nil
}

// stringOrEmpty is the string value points to, or "" when it is nil, so a missing attribute is
// stored as "".
func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// addWorkspaceRelationship links the workspace to the object with the given id, of type name, when
// the id is known. A null id is never sent, so an existing link is kept.
func addWorkspaceRelationship(relationships map[string]terrakube.Relationship, name string, id types.String) {
	if id.IsNull() || id.IsUnknown() {
		return
	}
	value := id.ValueString()
	relationships[name] = terrakube.Relationship{Type: name, ID: &value}
}
