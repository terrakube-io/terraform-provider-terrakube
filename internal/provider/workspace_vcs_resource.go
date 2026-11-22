package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	terrakube "github.com/terrakube-io/terrakube-go"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource                = &WorkspaceVcsResource{}
	_ resource.ResourceWithImportState = &WorkspaceVcsResource{}
)

type WorkspaceVcsResource struct {
	client *terrakube.Client
}

type WorkspaceVcsResourceModel struct {
	RestrictApplyToConfiguredBranch types.Bool   `tfsdk:"restrict_apply_to_configured_branch"`
	RequireApproveToApply           types.Bool   `tfsdk:"require_approve_to_apply"`
	ID                              types.String `tfsdk:"id"`
	Name                            types.String `tfsdk:"name"`
	OrganizationId                  types.String `tfsdk:"organization_id"`
	Description                     types.String `tfsdk:"description"`
	IaCType                         types.String `tfsdk:"iac_type"`
	TemplateId                      types.String `tfsdk:"template_id"`
	IaCVersion                      types.String `tfsdk:"iac_version"`
	Repository                      types.String `tfsdk:"repository"`
	Branch                          types.String `tfsdk:"branch"`
	Folder                          types.String `tfsdk:"folder"`
	ExecutionMode                   types.String `tfsdk:"execution_mode"`
	AgentId                         types.String `tfsdk:"agent_id"`
	VcsId                           types.String `tfsdk:"vcs_id"`
	SshId                           types.String `tfsdk:"ssh_id"`
	AllowRemoteApply                types.Bool   `tfsdk:"allow_remote_apply"`
	ProjectId                       types.String `tfsdk:"project_id"`
	ModuleSshKey                    types.String `tfsdk:"module_ssh_key"`
	PolicyComplianceStatus          types.String `tfsdk:"policy_compliance_status"`
}

func NewWorkspaceVcsResource() resource.Resource {
	return &WorkspaceVcsResource{}
}

func (r *WorkspaceVcsResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_vcs"
}

func (r *WorkspaceVcsResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a VCS workspace for Terrakube. When running plan from UI with VCS workspace " +
			"it will compare files contained in github repository with the cloud provider. If you only want to compare state " +
			"with cloud provider API use CLI workspace instead.",

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
				Description: "Workspace VCS name",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Workspace VCS description",
			},
			"execution_mode": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("remote"),
				Description: "Workspace VCS execution mode (remote or local)",
				Validators: []validator.String{
					stringvalidator.OneOf("remote", "local"),
				},
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
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("terraform"),
				Description: "Workspace VCS IaC type (Supported values terraform or tofu)",
				Validators: []validator.String{
					stringvalidator.OneOf("terraform", "tofu"),
				},
			},
			"iac_version": schema.StringAttribute{
				Required:    true,
				Description: "Workspace VCS VCS type",
			},
			"repository": schema.StringAttribute{
				Required:    true,
				Description: "Workspace VCS repository",
			},
			"template_id": schema.StringAttribute{
				Required:    true,
				Description: "Default template ID for the workspace",
			},
			"branch": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("main"),
				Description: "Workspace VCS branch",
			},
			"folder": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("/"),
				Description: "Workspace VCS folder",
			},
			"vcs_id": schema.StringAttribute{
				Optional:    true,
				Description: "VCS connection ID for private workspaces",
				Validators: []validator.String{
					stringvalidator.ConflictsWith(path.MatchRoot("ssh_id")),
				},
			},
			"ssh_id": schema.StringAttribute{
				Optional: true,
				Description: "SSH key ID (see terrakube_ssh) used to clone the workspace repository directly over SSH " +
					"(e.g. git@github.com:org/repo.git), as an alternative to an OAuth-based VCS connection. Mutually " +
					"exclusive with vcs_id. Leave unset to leave any existing SSH key assignment untouched.",
				Validators: []validator.String{
					stringvalidator.ConflictsWith(path.MatchRoot("vcs_id")),
				},
			},
			"module_ssh_key": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "SSH key ID (see terrakube_ssh) used to download private Terraform/OpenTofu modules " +
					"referenced via git-based module sources within this workspace. This key is not used to clone " +
					"the workspace repository itself; use vcs_id or ssh_id for that. Leave unset to leave any " +
					"existing value untouched; set to an empty string to clear it.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"allow_remote_apply": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Wether to allow remote apply. By default false to respect VCS philosophy.",
			},
			"project_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Id of the project this workspace belongs to. Leave unset to leave any existing project assignment (e.g. made outside Terraform) untouched.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"restrict_apply_to_configured_branch": schema.BoolAttribute{
				Optional: true, Computed: true,
				Description:   "Restrict apply and destroy to the configured workspace branch (Terrakube 2.34.0+). API runs pinned to a commit ID cannot change infrastructure. Terraform CLI uploads of local configuration can only run ordinary terraform plan; terraform apply, terraform destroy, and terraform plan -destroy are refused. Plan-only runs can use other branches. Updating requires organization workspace management permission. Leave unset to preserve the server value (false on creation).",
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

func (r *WorkspaceVcsResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Workspace VCS Resource")
}

func (r *WorkspaceVcsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceVcsResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.createString("name", plan.Name)
	attributes.createString("description", plan.Description)
	attributes.createString("source", plan.Repository)
	attributes.createString("branch", plan.Branch)
	attributes.createString("folder", plan.Folder)
	attributes.createString("defaultTemplate", plan.TemplateId)
	attributes.createString("iacType", plan.IaCType)
	attributes.createString("terraformVersion", plan.IaCVersion)
	attributes.createString("executionMode", plan.ExecutionMode)
	attributes.createBool("allowRemoteApply", plan.AllowRemoteApply)
	attributes.createString("moduleSshKey", plan.ModuleSshKey)
	attributes.createBool("restrictApplyToConfiguredBranch", plan.RestrictApplyToConfiguredBranch)
	attributes.createBool("requireApproveToApply", plan.RequireApproveToApply)

	relationships := map[string]terrakube.Relationship{}
	addWorkspaceRelationship(relationships, "vcs", plan.VcsId)
	addWorkspaceRelationship(relationships, "ssh", plan.SshId)
	addWorkspaceRelationship(relationships, "agent", plan.AgentId)
	addWorkspaceRelationship(relationships, "project", plan.ProjectId)

	workspace, err := r.client.Workspaces.CreateFields(ctx, plan.OrganizationId.ValueString(), terrakube.Patch{Attributes: attributes, Relationships: relationships})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating workspace vcs", err)
		return
	}

	plan.ID = types.StringValue(workspace.ID)
	setWorkspaceVcsState(&plan, workspace)
	if !plan.VcsId.IsNull() && workspace.Vcs != nil {
		plan.VcsId = types.StringValue(workspace.Vcs.ID)
	}
	if !plan.Folder.IsNull() {
		plan.Folder = types.StringValue(workspace.Folder)
	}

	tflog.Info(ctx, "Workspace VCS Resource Created", map[string]any{"success": true})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceVcsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceVcsResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspace, err := r.client.Workspaces.Get(ctx, state.OrganizationId.ValueString(), state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Workspace VCS not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace vcs", err)
		return
	}

	state.ID = types.StringValue(workspace.ID)
	setWorkspaceVcsState(&state, workspace)
	setWorkspaceVcsRefreshedState(&state, workspace)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *WorkspaceVcsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan WorkspaceVcsResourceModel
	var state WorkspaceVcsResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Read config rather than refreshed state: omitted guards must not be written
	// by a workspace manager who lacks organization-level guard permissions.
	var config WorkspaceVcsResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.updateString("name", plan.Name, state.Name)
	attributes.updateString("description", plan.Description, state.Description)
	attributes.updateString("source", plan.Repository, state.Repository)
	attributes.updateString("branch", plan.Branch, state.Branch)
	attributes.updateString("folder", plan.Folder, state.Folder)
	attributes.updateString("defaultTemplate", plan.TemplateId, state.TemplateId)
	attributes.updateString("iacType", plan.IaCType, state.IaCType)
	attributes.updateString("terraformVersion", plan.IaCVersion, state.IaCVersion)
	attributes.updateString("executionMode", plan.ExecutionMode, state.ExecutionMode)
	attributes.updateBool("allowRemoteApply", plan.AllowRemoteApply, state.AllowRemoteApply)
	// As before, module_ssh_key is never cleared with null; an empty string clears it.
	attributes.createString("moduleSshKey", plan.ModuleSshKey)
	attributes.createBool("restrictApplyToConfiguredBranch", config.RestrictApplyToConfiguredBranch)
	attributes.createBool("requireApproveToApply", config.RequireApproveToApply)

	// As before, a vcs_id, ssh_id or agent_id removed from the configuration is not sent, so the
	// link stays on the server.
	relationships := map[string]terrakube.Relationship{}
	addWorkspaceRelationship(relationships, "vcs", plan.VcsId)
	addWorkspaceRelationship(relationships, "ssh", plan.SshId)
	addWorkspaceRelationship(relationships, "agent", plan.AgentId)
	// Resending an unchanged project invokes reassignment permissions in Elide.
	if !plan.ProjectId.Equal(state.ProjectId) {
		addWorkspaceRelationship(relationships, "project", plan.ProjectId)
	}

	organizationID, id := state.OrganizationId.ValueString(), state.ID.ValueString()
	_, err := r.client.Workspaces.UpdateFields(ctx, organizationID, id, terrakube.Patch{Attributes: attributes, Relationships: relationships})
	if patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating workspace vcs", err)
		return
	}

	workspace, err := r.client.Workspaces.Get(ctx, organizationID, id)
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace vcs after update", err)
		return
	}

	plan.ID = types.StringValue(id)
	setWorkspaceVcsState(&plan, workspace)
	setWorkspaceVcsRefreshedState(&plan, workspace)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete marks the workspace as deleted, as the Terrakube UI does, and renames it so the name can
// be used again.
func (r *WorkspaceVcsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data WorkspaceVcsResourceModel
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
		"deleted": true,
	}
	attributes.createString("description", data.Description)
	attributes.createString("source", data.Repository)
	attributes.createString("branch", data.Branch)
	attributes.createString("iacType", data.IaCType)
	attributes.createString("defaultTemplate", data.TemplateId)
	attributes.createString("terraformVersion", data.IaCVersion)
	attributes.createString("executionMode", data.ExecutionMode)
	attributes.createBool("allowRemoteApply", data.AllowRemoteApply)

	_, err = r.client.Workspaces.UpdateFields(ctx, data.OrganizationId.ValueString(), data.ID.ValueString(), terrakube.Patch{Attributes: attributes})
	if patchFailed(err) && !terrakube.IsNotFound(err) {
		reportClientError(&resp.Diagnostics, "deleting workspace vcs", err)
		return
	}

	tflog.Info(ctx, "Workspace VCS Resource deleted", map[string]any{"success": true})
}

func (r *WorkspaceVcsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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

// setWorkspaceVcsState copies the attributes Terrakube returned into the model, except vcs_id and
// folder: Create, Read and Update each set those their own way.
func setWorkspaceVcsState(model *WorkspaceVcsResourceModel, workspace *terrakube.Workspace) {
	model.Name = types.StringValue(workspace.Name)
	model.Description = types.StringPointerValue(workspace.Description)
	model.Repository = types.StringValue(workspace.Source)
	model.Branch = types.StringValue(workspace.Branch)
	model.IaCType = types.StringValue(workspace.IaCType)
	model.IaCVersion = types.StringValue(workspace.IaCVersion)
	model.TemplateId = types.StringValue(workspace.TemplateID)
	model.ExecutionMode = types.StringValue(workspace.ExecutionMode)
	model.AllowRemoteApply = types.BoolValue(workspace.AllowRemoteApply)
	model.ProjectId = types.StringNull()
	if workspace.Project != nil {
		model.ProjectId = types.StringValue(workspace.Project.ID)
	}
	model.AgentId = types.StringNull()
	if workspace.Agent != nil {
		model.AgentId = types.StringValue(workspace.Agent.ID)
	}
	model.SshId = types.StringNull()
	if workspace.SSH != nil {
		model.SshId = types.StringValue(workspace.SSH.ID)
	}
	model.ModuleSshKey = types.StringPointerValue(workspace.ModuleSSHKey)
	model.PolicyComplianceStatus = types.StringValue(workspacePolicyComplianceStatus(workspace))
	model.RestrictApplyToConfiguredBranch = types.BoolPointerValue(workspace.RestrictApplyToConfiguredBranch)
	model.RequireApproveToApply = types.BoolPointerValue(workspace.RequireApproveToApply)
}

// setWorkspaceVcsRefreshedState sets folder and vcs_id after a read. A missing vcs relationship
// keeps the previous vcs_id.
func setWorkspaceVcsRefreshedState(model *WorkspaceVcsResourceModel, workspace *terrakube.Workspace) {
	model.Folder = types.StringValue(workspace.Folder)
	if workspace.Vcs != nil {
		model.VcsId = types.StringValue(workspace.Vcs.ID)
	}
}
