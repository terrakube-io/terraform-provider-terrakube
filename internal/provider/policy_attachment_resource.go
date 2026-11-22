package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	terrakube "github.com/terrakube-io/terrakube-go"
)

var _ resource.Resource = &PolicyAttachmentResource{}
var _ resource.ResourceWithConfigure = &PolicyAttachmentResource{}
var _ resource.ResourceWithImportState = &PolicyAttachmentResource{}

type PolicyAttachmentResource struct {
	client *terrakube.Client
}

type PolicyAttachmentResourceModel struct {
	ID          types.String `tfsdk:"id"`
	PolicySetId types.String `tfsdk:"policy_set_id"`
	WorkspaceId types.String `tfsdk:"workspace_id"`
	ProjectId   types.String `tfsdk:"project_id"`
	TagId       types.String `tfsdk:"tag_id"`
}

func NewPolicyAttachmentResource() resource.Resource {
	return &PolicyAttachmentResource{}
}

func (r *PolicyAttachmentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_policy_attachment"
}

func (r *PolicyAttachmentResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Attach a Terrakube Policy Set to a workspace, project, or tag scope. Workspaces evaluate the cumulative union of all policy sets attached globally, to their parent project, to their assigned tags, and directly to the workspace.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Attachment ID (UUID)",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"policy_set_id": schema.StringAttribute{
				Required:    true,
				Description: "ID of the target Policy Set. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Optional:    true,
				Description: "Workspace ID to attach the policy set to. Mutually exclusive with project_id and tag_id. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"project_id": schema.StringAttribute{
				Optional:    true,
				Description: "Project ID to attach the policy set to all workspaces in the project. Mutually exclusive with workspace_id and tag_id. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"tag_id": schema.StringAttribute{
				Optional:    true,
				Description: "Organization Tag ID to attach the policy set to all workspaces bearing the tag. Mutually exclusive with workspace_id and project_id. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *PolicyAttachmentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Policy Attachment Resource")
}

func (r *PolicyAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PolicyAttachmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	scopesSet := 0
	if !plan.WorkspaceId.IsNull() && !plan.WorkspaceId.IsUnknown() && plan.WorkspaceId.ValueString() != "" {
		scopesSet++
	}
	if !plan.ProjectId.IsNull() && !plan.ProjectId.IsUnknown() && plan.ProjectId.ValueString() != "" {
		scopesSet++
	}
	if !plan.TagId.IsNull() && !plan.TagId.IsUnknown() && plan.TagId.ValueString() != "" {
		scopesSet++
	}

	if scopesSet != 1 {
		resp.Diagnostics.AddError(
			"Invalid Attachment Target",
			"Exactly one of 'workspace_id', 'project_id', or 'tag_id' must be specified for a policy attachment.",
		)
		return
	}

	policySetID := plan.PolicySetId.ValueString()
	relationships := map[string]terrakube.Relationship{"policySet": {Type: "policy_set", ID: &policySetID}}
	if !plan.WorkspaceId.IsNull() && plan.WorkspaceId.ValueString() != "" {
		workspaceID := plan.WorkspaceId.ValueString()
		relationships["workspace"] = terrakube.Relationship{Type: "workspace", ID: &workspaceID}
	}
	if !plan.ProjectId.IsNull() && plan.ProjectId.ValueString() != "" {
		projectID := plan.ProjectId.ValueString()
		relationships["project"] = terrakube.Relationship{Type: "project", ID: &projectID}
	}
	if !plan.TagId.IsNull() && plan.TagId.ValueString() != "" {
		tagID := plan.TagId.ValueString()
		relationships["tag"] = terrakube.Relationship{Type: "tag", ID: &tagID}
	}

	attachment, err := r.client.PolicyAttachments.CreateFieldsByPolicySet(ctx, policySetID, terrakube.Patch{Relationships: relationships})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating policy attachment", err)
		return
	}

	setPolicyAttachmentState(&plan, attachment)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PolicyAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PolicyAttachmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attachment, err := r.client.PolicyAttachments.GetByPolicySet(ctx, state.PolicySetId.ValueString(), state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Policy attachment not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading policy attachment", err)
		return
	}

	setPolicyAttachmentState(&state, attachment)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *PolicyAttachmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Attachments are immutable and have RequiresReplace on all attributes
	resp.Diagnostics.AddError("Update not supported", "Policy attachments are immutable; updates require replacement.")
}

func (r *PolicyAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PolicyAttachmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.PolicyAttachments.DeleteByPolicySet(ctx, state.PolicySetId.ValueString(), state.ID.ValueString())
	if reportDeleteError(&resp.Diagnostics, "deleting policy attachment", err) {
		return
	}

	tflog.Info(ctx, "Policy attachment deleted", map[string]any{"id": state.ID.ValueString()})
}

func setPolicyAttachmentState(model *PolicyAttachmentResourceModel, attachment *terrakube.PolicyAttachment) {
	model.ID = types.StringValue(attachment.ID)
	if attachment.Workspace != nil {
		model.WorkspaceId = types.StringValue(attachment.Workspace.ID)
	}
	if attachment.Project != nil {
		model.ProjectId = types.StringValue(attachment.Project.ID)
	}
	if attachment.Tag != nil {
		model.TagId = types.StringValue(attachment.Tag.ID)
	}
}

func (r *PolicyAttachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) != 2 || idParts[0] == "" || idParts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: 'policy_set_id,id', Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("policy_set_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[1])...)
}
