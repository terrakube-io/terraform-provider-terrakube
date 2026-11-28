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
var _ resource.Resource = &TeamResource{}
var _ resource.ResourceWithImportState = &TeamResource{}
var _ resource.ResourceWithConfigValidators = &TeamResource{}

type TeamResource struct {
	client *terrakube.Client
}

type TeamResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	OrganizationId   types.String `tfsdk:"organization_id"`
	ManageState      types.Bool   `tfsdk:"manage_state"`
	ManageWorkspace  types.Bool   `tfsdk:"manage_workspace"`
	ManageModule     types.Bool   `tfsdk:"manage_module"`
	ManageProvider   types.Bool   `tfsdk:"manage_provider"`
	ManageVcs        types.Bool   `tfsdk:"manage_vcs"`
	ManageTemplate   types.Bool   `tfsdk:"manage_template"`
	ManageJob        types.Bool   `tfsdk:"manage_job"`
	ManageCollection types.Bool   `tfsdk:"manage_collection"`
	PlanJob          types.Bool   `tfsdk:"plan_job"`
	ApproveJob       types.Bool   `tfsdk:"approve_job"`
	DestroyJob       types.Bool   `tfsdk:"destroy_job"`
	Role             types.String `tfsdk:"role"`
	ManagePolicies   types.Bool   `tfsdk:"manage_policies"`
}

func NewTeamResource() resource.Resource {
	return &TeamResource{}
}

func (r *TeamResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{rbacRoleConflictValidator{}}
}

func (r *TeamResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_team"
}

func (r *TeamResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a team and bind it to an organization. Allows for fine-grained access management.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Team Id",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization id",
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
			"manage_collection": schema.BoolAttribute{
				Optional:    true,
				Description: "Allow to manage variables collection",
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"manage_workspace": schema.BoolAttribute{
				Optional:    true,
				Description: "Allow to manage workspaces",
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"manage_module": schema.BoolAttribute{
				Optional:    true,
				Description: "Allow to manage modules",
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"manage_provider": schema.BoolAttribute{
				Optional:    true,
				Description: "Allow to manage providers",
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"manage_vcs": schema.BoolAttribute{
				Optional:    true,
				Description: "Allow to manage vcs connections",
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"manage_template": schema.BoolAttribute{
				Optional:    true,
				Description: "Allow to manage templates",
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
			"manage_policies": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Allow managing OPA policy sets, attachments, and exemptions. Backward-compatible: omitted in requests to older Terrakube APIs when unset.",
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *TeamResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Team Resource")
}

func (r *TeamResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan TeamResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	team, err := r.client.Teams.CreateFields(ctx, plan.OrganizationId.ValueString(), terrakube.Patch{Attributes: teamAttributes(plan)})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating team", err)
		return
	}

	plan.ID = types.StringValue(team.ID)
	setTeamState(&plan, team)

	tflog.Info(ctx, "Team Resource Created", map[string]any{"success": true})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *TeamResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state TeamResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	team, err := r.client.Teams.Get(ctx, state.OrganizationId.ValueString(), state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Team not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading team", err)
		return
	}

	setTeamState(&state, team)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Team Resource reading", map[string]any{"success": true})
}

func (r *TeamResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan TeamResourceModel
	var state TeamResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organizationID, id := state.OrganizationId.ValueString(), state.ID.ValueString()
	_, err := r.client.Teams.UpdateFields(ctx, organizationID, id, terrakube.Patch{Attributes: teamAttributes(plan)})
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Team not found during update, removing from state", map[string]any{"id": id})
		resp.State.RemoveResource(ctx)
		return
	}
	if patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating team", err)
		return
	}

	team, err := r.client.Teams.Get(ctx, organizationID, id)
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Team not found after update, removing from state", map[string]any{"id": id})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading team after update", err)
		return
	}

	plan.ID = types.StringValue(id)
	setTeamState(&plan, team)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *TeamResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data TeamResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Teams.Delete(ctx, data.OrganizationId.ValueString(), data.ID.ValueString())
	if err != nil && !terrakube.IsNotFound(err) {
		reportClientError(&resp.Diagnostics, "deleting team", err)
	}
}

// teamAttributes builds the attributes sent on create and on update. The permission flags are
// always sent, plan_job and approve_job inheriting manage_job when unset. destroy_job, role and
// manage_policies are sent only when known, never as null: older APIs reject them.
func teamAttributes(plan TeamResourceModel) patchAttributes {
	attributes := patchAttributes{
		"manageState":      plan.ManageState.ValueBool(),
		"manageWorkspace":  plan.ManageWorkspace.ValueBool(),
		"manageModule":     plan.ManageModule.ValueBool(),
		"manageProvider":   plan.ManageProvider.ValueBool(),
		"manageTemplate":   plan.ManageTemplate.ValueBool(),
		"manageVcs":        plan.ManageVcs.ValueBool(),
		"manageJob":        plan.ManageJob.ValueBool(),
		"manageCollection": plan.ManageCollection.ValueBool(),
		"planJob":          resolveJobFlag(plan.PlanJob, plan.ManageJob),
		"approveJob":       resolveJobFlag(plan.ApproveJob, plan.ManageJob),
	}
	attributes.createString("name", plan.Name)
	attributes.createBool("destroyJob", plan.DestroyJob)
	attributes.createString("role", plan.Role)
	attributes.createBool("managePolicies", plan.ManagePolicies)
	return attributes
}

// setTeamState copies the team the API returned into model, all but the ID.
func setTeamState(model *TeamResourceModel, team *terrakube.Team) {
	model.Name = types.StringValue(team.Name)
	model.ManageState = types.BoolValue(team.ManageState)
	model.ManageWorkspace = types.BoolValue(team.ManageWorkspace)
	model.ManageModule = types.BoolValue(team.ManageModule)
	model.ManageVcs = types.BoolValue(team.ManageVcs)
	model.ManageProvider = types.BoolValue(team.ManageProvider)
	model.ManageTemplate = types.BoolValue(team.ManageTemplate)
	model.ManageJob = types.BoolValue(team.ManageJob)
	model.ManageCollection = types.BoolValue(team.ManageCollection)
	model.PlanJob = types.BoolValue(team.PlanJob)
	model.ApproveJob = types.BoolValue(team.ApproveJob)
	model.DestroyJob = types.BoolPointerValue(team.DestroyJob)
	model.Role = roleToState(&team.Role)
	model.ManagePolicies = types.BoolPointerValue(team.ManagePolicies)
}

func (r *TeamResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) != 2 || idParts[0] == "" || idParts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: 'organization_ID,ID', Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[1])...)
}
