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

var _ resource.Resource = &PolicyExemptionResource{}
var _ resource.ResourceWithConfigure = &PolicyExemptionResource{}
var _ resource.ResourceWithImportState = &PolicyExemptionResource{}

type PolicyExemptionResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type PolicyExemptionResourceModel struct {
	ID              types.String `tfsdk:"id"`
	OrganizationId  types.String `tfsdk:"organization_id"`
	PolicySetId     types.String `tfsdk:"policy_set_id"`
	RuleId          types.String `tfsdk:"rule_id"`
	TicketReference types.String `tfsdk:"ticket_reference"`
	Justification   types.String `tfsdk:"justification"`
	ExpiresAt       types.String `tfsdk:"expires_at"`
	WorkspaceId     types.String `tfsdk:"workspace_id"`
	ProjectId       types.String `tfsdk:"project_id"`
}

func NewPolicyExemptionResource() resource.Resource {
	return &PolicyExemptionResource{}
}

func (r *PolicyExemptionResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_policy_exemption"
}

func (r *PolicyExemptionResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create and manage a time-bounded OPA Policy Rule Exemption in Terrakube. Exemptions allow specific rules to be safely bypassed with an audit trail (ticket reference and justification) for designated workspaces or projects until an expiration date.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Exemption ID (UUID)",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization ID",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"policy_set_id": schema.StringAttribute{
				Required:    true,
				Description: "Target Policy Set ID containing the rule to exempt",
			},
			"rule_id": schema.StringAttribute{
				Required:    true,
				Description: "OPA Rego rule ID or package path (e.g. azure_apim_no_public_network)",
			},
			"ticket_reference": schema.StringAttribute{
				Required:    true,
				Description: "Ticketing system reference (e.g. Jira SEC-8842, ServiceNow RITM10293)",
			},
			"justification": schema.StringAttribute{
				Required:    true,
				Description: "Detailed justification explaining why the policy rule is exempted",
			},
			"expires_at": schema.StringAttribute{
				Optional:    true,
				Description: "Expiration timestamp in RFC3339 format (e.g. 2026-12-31T23:59:59Z). After this date, the exemption automatically deactivates.",
			},
			"workspace_id": schema.StringAttribute{
				Optional:    true,
				Description: "Optional workspace ID to restrict the exemption to a specific workspace",
			},
			"project_id": schema.StringAttribute{
				Optional:    true,
				Description: "Optional project ID to restrict the exemption to all workspaces in a project",
			},
		},
	}
}

func (r *PolicyExemptionResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Policy Exemption Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Policy Exemption resource", map[string]any{"success": true})
}

func (r *PolicyExemptionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PolicyExemptionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.PolicyExemptionEntity{
		PolicySet:       &client.PolicySetEntity{ID: plan.PolicySetId.ValueString()},
		RuleId:          plan.RuleId.ValueString(),
		TicketReference: plan.TicketReference.ValueString(),
		Justification:   plan.Justification.ValueString(),
		ExpiresAt:       plan.ExpiresAt.ValueStringPointer(),
	}

	if !plan.WorkspaceId.IsNull() && !plan.WorkspaceId.IsUnknown() && plan.WorkspaceId.ValueString() != "" {
		bodyRequest.Workspace = &client.WorkspaceEntity{ID: plan.WorkspaceId.ValueString()}
	}

	if !plan.ProjectId.IsNull() && !plan.ProjectId.IsUnknown() && plan.ProjectId.ValueString() != "" {
		bodyRequest.Project = &client.ProjectEntity{ID: plan.ProjectId.ValueString()}
	}

	var out bytes.Buffer
	if err := jsonapi.MarshalPayload(&out, bodyRequest); err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	url := fmt.Sprintf("%s/api/v1/organization/%s/policyExemption", r.endpoint, plan.OrganizationId.ValueString())
	createReq, err := http.NewRequest(http.MethodPost, url, strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating policy exemption request", fmt.Sprintf("Error: %s", err))
		return
	}
	createReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	createReq.Header.Add("Content-Type", "application/vnd.api+json")

	createResp, err := r.client.Do(createReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing policy exemption create request", fmt.Sprintf("Error: %s", err))
		return
	}
	defer createResp.Body.Close()

	bodyResponse, err := io.ReadAll(createResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading policy exemption response body", fmt.Sprintf("Error: %s", err))
		return
	}

	if createResp.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error creating policy exemption", fmt.Sprintf("status: %v, body: %v", createResp.Status, string(bodyResponse)))
		return
	}

	newExemption := &client.PolicyExemptionEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), newExemption); err != nil {
		resp.Diagnostics.AddError("Error unmarshalling policy exemption payload", fmt.Sprintf("Error: %s", err))
		return
	}

	plan.ID = types.StringValue(newExemption.ID)
	plan.RuleId = types.StringValue(newExemption.RuleId)
	plan.TicketReference = types.StringValue(newExemption.TicketReference)
	plan.Justification = types.StringValue(newExemption.Justification)
	plan.ExpiresAt = types.StringPointerValue(newExemption.ExpiresAt)
	if newExemption.PolicySet != nil {
		plan.PolicySetId = types.StringValue(newExemption.PolicySet.ID)
	}
	if newExemption.Workspace != nil {
		plan.WorkspaceId = types.StringValue(newExemption.Workspace.ID)
	}
	if newExemption.Project != nil {
		plan.ProjectId = types.StringValue(newExemption.Project.ID)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PolicyExemptionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PolicyExemptionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	url := fmt.Sprintf("%s/api/v1/policy_exemption/%s", r.endpoint, state.ID.ValueString())
	readReq, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating policy exemption read request", fmt.Sprintf("Error: %s", err))
		return
	}
	readReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	readReq.Header.Add("Content-Type", "application/vnd.api+json")

	readResp, err := r.client.Do(readReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing policy exemption read request", fmt.Sprintf("Error: %s", err))
		return
	}
	defer readResp.Body.Close()

	if readResp.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Policy exemption not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	bodyResponse, err := io.ReadAll(readResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading policy exemption response body", fmt.Sprintf("Error: %s", err))
		return
	}

	if readResp.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error reading policy exemption", fmt.Sprintf("status: %v, body: %v", readResp.Status, string(bodyResponse)))
		return
	}

	exemption := &client.PolicyExemptionEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), exemption); err != nil {
		resp.Diagnostics.AddError("Error unmarshalling policy exemption payload", fmt.Sprintf("Error: %s", err))
		return
	}

	state.ID = types.StringValue(exemption.ID)
	state.RuleId = types.StringValue(exemption.RuleId)
	state.TicketReference = types.StringValue(exemption.TicketReference)
	state.Justification = types.StringValue(exemption.Justification)
	state.ExpiresAt = types.StringPointerValue(exemption.ExpiresAt)
	if exemption.PolicySet != nil {
		state.PolicySetId = types.StringValue(exemption.PolicySet.ID)
	}
	if exemption.Workspace != nil {
		state.WorkspaceId = types.StringValue(exemption.Workspace.ID)
	}
	if exemption.Project != nil {
		state.ProjectId = types.StringValue(exemption.Project.ID)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *PolicyExemptionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan PolicyExemptionResourceModel
	var state PolicyExemptionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.PolicyExemptionEntity{
		ID:              state.ID.ValueString(),
		PolicySet:       &client.PolicySetEntity{ID: plan.PolicySetId.ValueString()},
		RuleId:          plan.RuleId.ValueString(),
		TicketReference: plan.TicketReference.ValueString(),
		Justification:   plan.Justification.ValueString(),
		ExpiresAt:       plan.ExpiresAt.ValueStringPointer(),
	}

	if !plan.WorkspaceId.IsNull() && !plan.WorkspaceId.IsUnknown() && plan.WorkspaceId.ValueString() != "" {
		bodyRequest.Workspace = &client.WorkspaceEntity{ID: plan.WorkspaceId.ValueString()}
	}

	if !plan.ProjectId.IsNull() && !plan.ProjectId.IsUnknown() && plan.ProjectId.ValueString() != "" {
		bodyRequest.Project = &client.ProjectEntity{ID: plan.ProjectId.ValueString()}
	}

	var out bytes.Buffer
	if err := jsonapi.MarshalPayload(&out, bodyRequest); err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	url := fmt.Sprintf("%s/api/v1/policy_exemption/%s", r.endpoint, state.ID.ValueString())
	updateReq, err := http.NewRequest(http.MethodPatch, url, strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating policy exemption update request", fmt.Sprintf("Error: %s", err))
		return
	}
	updateReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	updateReq.Header.Add("Content-Type", "application/vnd.api+json")

	updateResp, err := r.client.Do(updateReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing policy exemption update request", fmt.Sprintf("Error: %s", err))
		return
	}
	defer updateResp.Body.Close()

	bodyResponse, err := io.ReadAll(updateResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading policy exemption update response body", fmt.Sprintf("Error: %s", err))
		return
	}

	if updateResp.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Policy exemption not found during update, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	if updateResp.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error updating policy exemption", fmt.Sprintf("status: %v, body: %v", updateResp.Status, string(bodyResponse)))
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.OrganizationId = types.StringValue(state.OrganizationId.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PolicyExemptionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PolicyExemptionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	url := fmt.Sprintf("%s/api/v1/policy_exemption/%s", r.endpoint, state.ID.ValueString())
	delReq, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating policy exemption delete request", fmt.Sprintf("Error: %s", err))
		return
	}
	delReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))

	delResp, err := r.client.Do(delReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing policy exemption delete request", fmt.Sprintf("Error: %s", err))
		return
	}
	defer delResp.Body.Close()

	if delResp.StatusCode >= http.StatusBadRequest && delResp.StatusCode != http.StatusNotFound {
		bodyResponse, _ := io.ReadAll(delResp.Body)
		resp.Diagnostics.AddError("Error deleting policy exemption", fmt.Sprintf("status: %v, body: %v", delResp.Status, string(bodyResponse)))
		return
	}

	tflog.Info(ctx, "Policy exemption deleted", map[string]any{"id": state.ID.ValueString()})
}

func (r *PolicyExemptionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) == 2 {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), idParts[0])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[1])...)
		return
	}

	if len(idParts) == 1 && idParts[0] != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[0])...)
		return
	}

	resp.Diagnostics.AddError(
		"Unexpected Import Identifier",
		fmt.Sprintf("Expected import identifier with format: 'organization_id,id' or 'id', Got: %q", req.ID),
	)
}
