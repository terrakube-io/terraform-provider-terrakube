package provider

import (
	"context"
	"fmt"
	"unicode/utf16"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ErrorResponse struct {
	Errors []struct {
		Detail string `json:"detail"`
	} `json:"errors"`
}

type AtomicOperationResponse struct {
	AtomicResults []struct {
		Data struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"data"`
	} `json:"atomic:results"`
}

// roleToState converts a nullable API role string to a Terraform string value.
// nil or empty string becomes null — both represent "unset/custom" on the server.
func roleToState(r *string) types.String {
	if r == nil || *r == "" {
		return types.StringNull()
	}
	return types.StringValue(*r)
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

// optionalBoolPointer preserves omission for attributes unsupported by older APIs.
func optionalBoolPointer(value types.Bool) *bool {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	return value.ValueBoolPointer()
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
