package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	terrakube "github.com/terrakube-io/terrakube-go"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &ProjectAccessResource{}
var _ resource.ResourceWithImportState = &ProjectAccessResource{}
var _ resource.ResourceWithConfigValidators = &ProjectAccessResource{}

type ProjectAccessResource struct {
	client *terrakube.Client
}

type ProjectAccessResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	OrganizationId  types.String `tfsdk:"organization_id"`
	ProjectId       types.String `tfsdk:"project_id"`
	ManageState     types.Bool   `tfsdk:"manage_state"`
	ManageWorkspace types.Bool   `tfsdk:"manage_workspace"`
	ManageJob       types.Bool   `tfsdk:"manage_job"`
	PlanJob         types.Bool   `tfsdk:"plan_job"`
	ApproveJob      types.Bool   `tfsdk:"approve_job"`
	DestroyJob      types.Bool   `tfsdk:"destroy_job"`
	Role            types.String `tfsdk:"role"`
}

func NewProjectAccessResource() resource.Resource {
	return &ProjectAccessResource{}
}

func (r *ProjectAccessResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{rbacRoleConflictValidator{}}
}

func (r *ProjectAccessResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_access"
}

func (r *ProjectAccessResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage project access. Grants a team RBAC permissions scoped to a project (and therefore to every workspace within it).",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Access Id",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization id",
			},
			"project_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube project id",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Team name. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"manage_state": schema.BoolAttribute{
				Optional:    true,
				Description: "Allow to manage Terraform/OpenTofu state",
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"manage_job": schema.BoolAttribute{
				Optional:    true,
				Description: "Allow to manage and trigger jobs. Legacy field — in RBAC v2, plan_job/approve_job inherit from this when unset.",
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"manage_workspace": schema.BoolAttribute{
				Optional:    true,
				Description: "Allow to create, update, and delete workspaces within the project",
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"plan_job": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Allow queuing plans (RBAC v2). Inherits manage_job when not set. Only used when role is unset or \"custom\". Note: inheritance only applies on create/update — imported resources retain the remote value.",
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"approve_job": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Allow approving/applying runs (RBAC v2). Inherits manage_job when not set. Only used when role is unset or \"custom\". Note: inheritance only applies on create/update — imported resources retain the remote value.",
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"destroy_job": schema.BoolAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Allow destroy runs when role is custom or unset (Terrakube 2.34.0+). This is an additional gate alongside plan/apply permissions. When unset, preserves the server value (true on creation); omitted for older APIs.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"role": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Predefined role: admin (all permissions), write (plan+apply+workspace+state), plan (plan only), read (read only), or custom (use boolean flags). When set to a non-custom value, overrides individual boolean flags. Leave unset to use boolean flags.",
				Validators: []validator.String{
					stringvalidator.OneOf("admin", "write", "plan", "read", "custom"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *ProjectAccessResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Project Access Resource")
}

func (r *ProjectAccessResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ProjectAccessResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	access, err := r.client.ProjectAccess.CreateFields(ctx, plan.OrganizationId.ValueString(), plan.ProjectId.ValueString(), terrakube.Patch{Attributes: projectAccessAttributes(plan)})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating project access", err)
		return
	}

	setProjectAccessState(&plan, access)
	plan.ID = types.StringValue(access.ID)

	tflog.Info(ctx, "project access Created", map[string]any{"success": true})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ProjectAccessResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ProjectAccessResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	access, err := r.client.ProjectAccess.Get(ctx, state.OrganizationId.ValueString(), state.ProjectId.ValueString(), state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Project access not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading project access", err)
		return
	}

	setProjectAccessState(&state, access)
	state.Name = types.StringValue(access.Name)
	state.ID = types.StringValue(access.ID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Project access Resource reading", map[string]any{"success": true})
}

func (r *ProjectAccessResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ProjectAccessResourceModel
	var state ProjectAccessResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organizationID, projectID, id := state.OrganizationId.ValueString(), state.ProjectId.ValueString(), state.ID.ValueString()
	_, err := r.client.ProjectAccess.UpdateFields(ctx, organizationID, projectID, id, terrakube.Patch{Attributes: projectAccessAttributes(plan)})
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Project access not found during update, removing from state", map[string]any{"id": id})
		resp.State.RemoveResource(ctx)
		return
	}
	if patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating project access", err)
		return
	}

	access, err := r.client.ProjectAccess.Get(ctx, organizationID, projectID, id)
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Project access not found after update, removing from state", map[string]any{"id": id})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading project access after update", err)
		return
	}

	plan.ID = types.StringValue(id)
	setProjectAccessState(&plan, access)
	plan.Name = types.StringValue(access.Name)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ProjectAccessResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ProjectAccessResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.ProjectAccess.Delete(ctx, data.OrganizationId.ValueString(), data.ProjectId.ValueString(), data.ID.ValueString())
	if err != nil && !terrakube.IsNotFound(err) {
		reportClientError(&resp.Diagnostics, "deleting project access", err)
	}
}

// projectAccessAttributes builds the attributes sent on create and on update. The permission flags are
// always sent, plan_job and approve_job inheriting manage_job when unset. destroy_job and role are
// sent only when known, never as null: older APIs reject destroy_job.
func projectAccessAttributes(plan ProjectAccessResourceModel) patchAttributes {
	attributes := patchAttributes{
		"manageState":     plan.ManageState.ValueBool(),
		"manageWorkspace": plan.ManageWorkspace.ValueBool(),
		"manageJob":       plan.ManageJob.ValueBool(),
		"planJob":         resolveJobFlag(plan.PlanJob, plan.ManageJob),
		"approveJob":      resolveJobFlag(plan.ApproveJob, plan.ManageJob),
	}
	attributes.createString("name", plan.Name)
	attributes.createBool("destroyJob", plan.DestroyJob)
	attributes.createString("role", plan.Role)
	return attributes
}

// setProjectAccessState copies the permissions the API returned into model.
func setProjectAccessState(model *ProjectAccessResourceModel, access *terrakube.ProjectAccess) {
	model.ManageState = types.BoolValue(access.ManageState)
	model.ManageWorkspace = types.BoolValue(access.ManageWorkspace)
	model.ManageJob = types.BoolValue(access.ManageJob)
	model.PlanJob = types.BoolValue(access.PlanJob)
	model.ApproveJob = types.BoolValue(access.ApproveJob)
	model.DestroyJob = optionalBoolState(access.DestroyJob, model.DestroyJob)
	model.Role = roleToState(&access.Role)
}

func (r *ProjectAccessResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) != 3 || idParts[0] == "" || idParts[1] == "" || idParts[2] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: 'organization_ID,project_ID,ID', Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), idParts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[2])...)
}
