package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	terrakube "github.com/terrakube-io/terrakube-go"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource                = &WorkspaceCliResource{}
	_ resource.ResourceWithImportState = &WorkspaceCliResource{}
)

type WorkspaceCliResource struct {
	client *terrakube.Client
}

type WorkspaceCliResourceModel struct {
	RestrictApplyToConfiguredBranch types.Bool   `tfsdk:"restrict_apply_to_configured_branch"`
	RequireApproveToApply           types.Bool   `tfsdk:"require_approve_to_apply"`
	ID                              types.String `tfsdk:"id"`
	Name                            types.String `tfsdk:"name"`
	OrganizationId                  types.String `tfsdk:"organization_id"`
	Description                     types.String `tfsdk:"description"`
	IaCType                         types.String `tfsdk:"iac_type"`
	IaCVersion                      types.String `tfsdk:"iac_version"`
	ExecutionMode                   types.String `tfsdk:"execution_mode"`
	AgentId                         types.String `tfsdk:"agent_id"`
	ProjectId                       types.String `tfsdk:"project_id"`
	ModuleSshKey                    types.String `tfsdk:"module_ssh_key"`
	PolicyComplianceStatus          types.String `tfsdk:"policy_compliance_status"`
}

func NewWorkspaceCliResource() resource.Resource {
	return &WorkspaceCliResource{}
}

func (r *WorkspaceCliResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_cli"
}

func (r *WorkspaceCliResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a CLI workspace for Terrakube. When running plan from UI with CLI workspace " +
			"only the current state will be compared to the cloud provider API not taking into account the file contained" +
			"in workspace working directory. If you want to fetch files from github use vcs_workspace instead.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Workspace CLI Id",
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
				Description: "Workspace CLI name",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Workspace CLI description",
			},
			"execution_mode": schema.StringAttribute{
				Required:    true,
				Description: "Workspace CLI execution mode (remote or local). Remote execution will require setting up executor.",
			},
			"agent_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Workspace executor agent ID. If you leave it empty, it defaults to the organization's default executor.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"iac_type": schema.StringAttribute{
				Required:    true,
				Description: "Workspace CLI IaC type (Supported values terraform or tofu)",
			},
			"iac_version": schema.StringAttribute{
				Required:    true,
				Description: "Workspace CLI IaC type",
			},
			"project_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Id of the project this workspace belongs to. Leave unset to leave any existing project assignment (e.g. made outside Terraform) untouched.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"module_ssh_key": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "SSH key ID (see terrakube_ssh) used to download private Terraform/OpenTofu modules " +
					"referenced via git-based module sources within this workspace. Leave unset to leave any " +
					"existing value untouched; set to an empty string to clear it.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"restrict_apply_to_configured_branch": schema.BoolAttribute{
				Optional: true, Computed: true,
				Description:   "Restrict infrastructure-changing runs to the configured workspace branch (Terrakube 2.34.0+). CLI workspaces use uploaded configuration rather than a VCS branch. Enabling this guard permits ordinary terraform plan but refuses terraform apply, terraform destroy, and terraform plan -destroy through the Terraform CLI. Updating requires organization workspace management permission. Leave unset to preserve the server value (false on creation).",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"require_approve_to_apply": schema.BoolAttribute{
				Optional: true, Computed: true,
				Description:   "Require approval permission to start an apply without a preceding approval step (Terrakube 2.34.0+). Updating requires organization workspace management permission. Leave unset to preserve the server value (false on creation).",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"policy_compliance_status": schema.StringAttribute{
				Computed:    true,
				Description: "OPA Policy compliance status: UNKNOWN, COMPLIANT, NON_COMPLIANT, or EXEMPTED",
			},
		},
	}
}

func (r *WorkspaceCliResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Workspace CLI Resource")
}

func (r *WorkspaceCliResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceCliResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{
		"source": workspaceCliSource,
		"branch": workspaceCliBranch,
	}
	attributes.createString("name", plan.Name)
	attributes.createString("description", plan.Description)
	attributes.createString("iacType", plan.IaCType)
	attributes.createString("terraformVersion", plan.IaCVersion)
	attributes.createString("executionMode", plan.ExecutionMode)
	attributes.createString("moduleSshKey", plan.ModuleSshKey)
	attributes.createBool("restrictApplyToConfiguredBranch", plan.RestrictApplyToConfiguredBranch)
	attributes.createBool("requireApproveToApply", plan.RequireApproveToApply)

	relationships := map[string]terrakube.Relationship{}
	addWorkspaceRelationship(relationships, "project", plan.ProjectId)
	addWorkspaceRelationship(relationships, "agent", plan.AgentId)

	workspace, err := r.client.Workspaces.CreateFields(ctx, plan.OrganizationId.ValueString(), terrakube.Patch{Attributes: attributes, Relationships: relationships})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating workspace", err)
		return
	}

	plan.ID = types.StringValue(workspace.ID)
	setWorkspaceCliState(&plan, workspace)

	tflog.Info(ctx, "Workspace Cli Resource Created", map[string]any{"success": true})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceCliResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceCliResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspace, err := r.client.Workspaces.Get(ctx, state.OrganizationId.ValueString(), state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Workspace CLI not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace", err)
		return
	}

	state.ID = types.StringValue(workspace.ID)
	setWorkspaceCliState(&state, workspace)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *WorkspaceCliResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan WorkspaceCliResourceModel
	var state WorkspaceCliResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Read config rather than refreshed state: omitted guards must not be written
	// by a workspace manager who lacks organization-level guard permissions.
	var config WorkspaceCliResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{
		"source": workspaceCliSource,
		"branch": workspaceCliBranch,
	}
	attributes.updateString("name", plan.Name, state.Name)
	attributes.updateString("description", plan.Description, state.Description)
	attributes.updateString("iacType", plan.IaCType, state.IaCType)
	attributes.updateString("terraformVersion", plan.IaCVersion, state.IaCVersion)
	attributes.updateString("executionMode", plan.ExecutionMode, state.ExecutionMode)
	// As before, module_ssh_key is never cleared with null; an empty string clears it.
	attributes.createString("moduleSshKey", plan.ModuleSshKey)
	attributes.createBool("restrictApplyToConfiguredBranch", config.RestrictApplyToConfiguredBranch)
	attributes.createBool("requireApproveToApply", config.RequireApproveToApply)

	relationships := map[string]terrakube.Relationship{}
	// Resending an unchanged project invokes reassignment permissions in Elide.
	if !plan.ProjectId.Equal(state.ProjectId) {
		addWorkspaceRelationship(relationships, "project", plan.ProjectId)
	}
	addWorkspaceRelationship(relationships, "agent", plan.AgentId)

	organizationID, id := state.OrganizationId.ValueString(), state.ID.ValueString()
	_, err := r.client.Workspaces.UpdateFields(ctx, organizationID, id, terrakube.Patch{Attributes: attributes, Relationships: relationships})
	if patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating workspace", err)
		return
	}

	workspace, err := r.client.Workspaces.Get(ctx, organizationID, id)
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace after update", err)
		return
	}

	plan.ID = types.StringValue(id)
	setWorkspaceCliState(&plan, workspace)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete marks the workspace as deleted, as the Terrakube UI does, and renames it so the name can
// be used again.
func (r *WorkspaceCliResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data WorkspaceCliResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name, err := randomSoftDeletedName(data.Name.ValueString(), 128)
	if err != nil {
		resp.Diagnostics.AddError("Error generating random string to delete workspace", fmt.Sprintf("Error generating random string to delete workspace: %s", err))
		return
	}

	attributes := patchAttributes{
		"name":    name,
		"source":  workspaceCliSource,
		"branch":  workspaceCliBranch,
		"deleted": true,
	}
	attributes.createString("description", data.Description)
	attributes.createString("iacType", data.IaCType)
	attributes.createString("terraformVersion", data.IaCVersion)
	attributes.createString("executionMode", data.ExecutionMode)

	_, err = r.client.Workspaces.UpdateFields(ctx, data.OrganizationId.ValueString(), data.ID.ValueString(), terrakube.Patch{Attributes: attributes})
	if patchFailed(err) && !terrakube.IsNotFound(err) {
		reportClientError(&resp.Diagnostics, "deleting workspace", err)
		return
	}

	tflog.Info(ctx, "Workspace Cli Resource deleted", map[string]any{"success": true})
}

func (r *WorkspaceCliResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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

// A CLI workspace has no repository: Terrakube marks it with this source and branch.
const (
	workspaceCliSource = "empty"
	workspaceCliBranch = "remote-content"
)

// setWorkspaceCliState copies the attributes Terrakube returned into the model.
func setWorkspaceCliState(model *WorkspaceCliResourceModel, workspace *terrakube.Workspace) {
	model.Name = types.StringValue(workspace.Name)
	model.Description = types.StringPointerValue(workspace.Description)
	model.IaCType = types.StringValue(workspace.IaCType)
	model.IaCVersion = types.StringValue(workspace.IaCVersion)
	model.ExecutionMode = types.StringValue(workspace.ExecutionMode)
	model.ProjectId = types.StringNull()
	if workspace.Project != nil {
		model.ProjectId = types.StringValue(workspace.Project.ID)
	}
	model.AgentId = types.StringNull()
	if workspace.Agent != nil {
		model.AgentId = types.StringValue(workspace.Agent.ID)
	}
	model.ModuleSshKey = types.StringPointerValue(workspace.ModuleSSHKey)
	model.PolicyComplianceStatus = types.StringValue(workspacePolicyComplianceStatus(workspace))
	model.RestrictApplyToConfiguredBranch = optionalBoolState(workspace.RestrictApplyToConfiguredBranch, model.RestrictApplyToConfiguredBranch)
	model.RequireApproveToApply = optionalBoolState(workspace.RequireApproveToApply, model.RequireApproveToApply)
}

// workspacePolicyComplianceStatus returns the workspace's policy compliance status, or "" when
// Terrakube did not send one, as before.
func workspacePolicyComplianceStatus(workspace *terrakube.Workspace) string {
	if workspace.PolicyComplianceStatus == nil {
		return ""
	}
	return *workspace.PolicyComplianceStatus
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
