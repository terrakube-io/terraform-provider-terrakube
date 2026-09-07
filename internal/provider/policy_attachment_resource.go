package provider

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"terraform-provider-terrakube/internal/client"

	"github.com/google/jsonapi"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ resource.Resource = &PolicyAttachmentResource{}
var _ resource.ResourceWithConfigure = &PolicyAttachmentResource{}
var _ resource.ResourceWithImportState = &PolicyAttachmentResource{}

type PolicyAttachmentResource struct {
	client   *http.Client
	endpoint string
	token    string
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
				Description: "ID of the target Policy Set",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Optional:    true,
				Description: "Workspace ID to attach the policy set to. Mutually exclusive with project_id and tag_id.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"project_id": schema.StringAttribute{
				Optional:    true,
				Description: "Project ID to attach the policy set to all workspaces in the project. Mutually exclusive with workspace_id and tag_id.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"tag_id": schema.StringAttribute{
				Optional:    true,
				Description: "Organization Tag ID to attach the policy set to all workspaces bearing the tag. Mutually exclusive with workspace_id and project_id.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *PolicyAttachmentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Policy Attachment Resource Configure Type",
			fmt.Sprintf("Expected *TerrakubeConnectionData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	if providerData.InsecureHttpClient {
		if custom, ok := http.DefaultTransport.(*http.Transport); ok {
			customTransport := custom.Clone()
			customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
			r.client = &http.Client{Transport: customTransport}
		} else {
			r.client = &http.Client{}
		}
	} else {
		r.client = &http.Client{}
	}

	r.endpoint = providerData.Endpoint
	r.token = providerData.Token

	tflog.Debug(ctx, "Configuring Policy Attachment resource", map[string]any{"success": true})
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

	bodyRequest := &client.PolicyAttachmentEntity{
		PolicySet: &client.PolicySetEntity{ID: plan.PolicySetId.ValueString()},
	}

	if !plan.WorkspaceId.IsNull() && plan.WorkspaceId.ValueString() != "" {
		bodyRequest.Workspace = &client.WorkspaceEntity{ID: plan.WorkspaceId.ValueString()}
	}
	if !plan.ProjectId.IsNull() && plan.ProjectId.ValueString() != "" {
		bodyRequest.Project = &client.ProjectEntity{ID: plan.ProjectId.ValueString()}
	}
	if !plan.TagId.IsNull() && plan.TagId.ValueString() != "" {
		bodyRequest.Tag = &client.OrganizationTagEntity{ID: plan.TagId.ValueString()}
	}

	var out bytes.Buffer
	if err := jsonapi.MarshalPayload(&out, bodyRequest); err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	url := fmt.Sprintf("%s/api/v1/policy_set/%s/attachments", r.endpoint, plan.PolicySetId.ValueString())
	createReq, err := http.NewRequest(http.MethodPost, url, strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating policy attachment request", fmt.Sprintf("Error: %s", err))
		return
	}
	createReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	createReq.Header.Add("Content-Type", "application/vnd.api+json")

	createResp, err := r.client.Do(createReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing policy attachment create request", fmt.Sprintf("Error: %s", err))
		return
	}
	defer createResp.Body.Close()

	bodyResponse, err := io.ReadAll(createResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading policy attachment response body", fmt.Sprintf("Error: %s", err))
		return
	}

	if createResp.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error creating policy attachment", fmt.Sprintf("status: %v, body: %v", createResp.Status, string(bodyResponse)))
		return
	}

	newAttachment := &client.PolicyAttachmentEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), newAttachment); err != nil {
		resp.Diagnostics.AddError("Error unmarshalling policy attachment payload", fmt.Sprintf("Error: %s", err))
		return
	}

	plan.ID = types.StringValue(newAttachment.ID)
	if newAttachment.Workspace != nil {
		plan.WorkspaceId = types.StringValue(newAttachment.Workspace.ID)
	}
	if newAttachment.Project != nil {
		plan.ProjectId = types.StringValue(newAttachment.Project.ID)
	}
	if newAttachment.Tag != nil {
		plan.TagId = types.StringValue(newAttachment.Tag.ID)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PolicyAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PolicyAttachmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	url := fmt.Sprintf("%s/api/v1/policy_set/%s/attachments/%s", r.endpoint, state.PolicySetId.ValueString(), state.ID.ValueString())
	readReq, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating policy attachment read request", fmt.Sprintf("Error: %s", err))
		return
	}
	readReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	readReq.Header.Add("Content-Type", "application/vnd.api+json")

	readResp, err := r.client.Do(readReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing policy attachment read request", fmt.Sprintf("Error: %s", err))
		return
	}
	defer readResp.Body.Close()

	if readResp.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Policy attachment not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	bodyResponse, err := io.ReadAll(readResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading policy attachment response body", fmt.Sprintf("Error: %s", err))
		return
	}

	if readResp.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error reading policy attachment", fmt.Sprintf("status: %v, body: %v", readResp.Status, string(bodyResponse)))
		return
	}

	attachment := &client.PolicyAttachmentEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), attachment); err != nil {
		resp.Diagnostics.AddError("Error unmarshalling policy attachment payload", fmt.Sprintf("Error: %s", err))
		return
	}

	state.ID = types.StringValue(attachment.ID)
	if attachment.Workspace != nil {
		state.WorkspaceId = types.StringValue(attachment.Workspace.ID)
	}
	if attachment.Project != nil {
		state.ProjectId = types.StringValue(attachment.Project.ID)
	}
	if attachment.Tag != nil {
		state.TagId = types.StringValue(attachment.Tag.ID)
	}

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

	url := fmt.Sprintf("%s/api/v1/policy_set/%s/attachments/%s", r.endpoint, state.PolicySetId.ValueString(), state.ID.ValueString())
	delReq, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating policy attachment delete request", fmt.Sprintf("Error: %s", err))
		return
	}
	delReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))

	delResp, err := r.client.Do(delReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing policy attachment delete request", fmt.Sprintf("Error: %s", err))
		return
	}
	defer delResp.Body.Close()

	if delResp.StatusCode >= http.StatusBadRequest && delResp.StatusCode != http.StatusNotFound {
		bodyResponse, _ := io.ReadAll(delResp.Body)
		resp.Diagnostics.AddError("Error deleting policy attachment", fmt.Sprintf("status: %v, body: %v", delResp.Status, string(bodyResponse)))
		return
	}

	tflog.Info(ctx, "Policy attachment deleted", map[string]any{"id": state.ID.ValueString()})
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
