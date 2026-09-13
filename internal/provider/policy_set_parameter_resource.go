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

var _ resource.Resource = &PolicySetParameterResource{}
var _ resource.ResourceWithConfigure = &PolicySetParameterResource{}
var _ resource.ResourceWithImportState = &PolicySetParameterResource{}

type PolicySetParameterResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type PolicySetParameterResourceModel struct {
	ID          types.String `tfsdk:"id"`
	PolicySetId types.String `tfsdk:"policy_set_id"`
	Key         types.String `tfsdk:"key"`
	Value       types.String `tfsdk:"value"`
	Description types.String `tfsdk:"description"`
}

func NewPolicySetParameterResource() resource.Resource {
	return &PolicySetParameterResource{}
}

func (r *PolicySetParameterResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_policy_set_parameter"
}

func (r *PolicySetParameterResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create and manage an OPA Policy Set Parameter in Terrakube. Parameters are key-value inputs passed directly to Rego policy evaluation at runtime under `data.terrakube.inputs.<key>`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Parameter ID (UUID)",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"policy_set_id": schema.StringAttribute{
				Required:    true,
				Description: "Target Policy Set ID",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"key": schema.StringAttribute{
				Required:    true,
				Description: "Parameter key identifier, referenced in Rego as data.terrakube.inputs.<key>",
			},
			"value": schema.StringAttribute{
				Required:    true,
				Description: "Parameter value (supports strings, numbers, booleans, and JSON structures)",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Parameter description or documentation",
			},
		},
	}
}

func (r *PolicySetParameterResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Policy Set Parameter Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Policy Set Parameter resource", map[string]any{"success": true})
}

func (r *PolicySetParameterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PolicySetParameterResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.PolicySetParameterEntity{
		Key:       plan.Key.ValueString(),
		Value:     plan.Value.ValueString(),
		PolicySet: &client.PolicySetEntity{ID: plan.PolicySetId.ValueString()},
	}

	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		desc := plan.Description.ValueString()
		bodyRequest.Description = &desc
	}

	var out bytes.Buffer
	if err := jsonapi.MarshalPayload(&out, bodyRequest); err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	url := fmt.Sprintf("%s/api/v1/policy_set/%s/parameters", r.endpoint, plan.PolicySetId.ValueString())
	createReq, err := http.NewRequest(http.MethodPost, url, strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating policy set parameter request", fmt.Sprintf("Error: %s", err))
		return
	}
	createReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	createReq.Header.Add("Content-Type", "application/vnd.api+json")

	createResp, err := r.client.Do(createReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing policy set parameter create request", fmt.Sprintf("Error: %s", err))
		return
	}
	defer createResp.Body.Close()

	bodyResponse, err := io.ReadAll(createResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading policy set parameter response body", fmt.Sprintf("Error: %s", err))
		return
	}

	if createResp.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error creating policy set parameter", fmt.Sprintf("status: %v, body: %v", createResp.Status, string(bodyResponse)))
		return
	}

	newParameter := &client.PolicySetParameterEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), newParameter); err != nil {
		resp.Diagnostics.AddError("Error unmarshalling policy set parameter payload", fmt.Sprintf("Error: %s", err))
		return
	}

	plan.ID = types.StringValue(newParameter.ID)
	plan.Key = types.StringValue(newParameter.Key)
	plan.Value = types.StringValue(newParameter.Value)
	if newParameter.Description != nil {
		plan.Description = types.StringValue(*newParameter.Description)
	} else {
		plan.Description = types.StringNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PolicySetParameterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PolicySetParameterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	url := fmt.Sprintf("%s/api/v1/policy_set/%s/parameters/%s", r.endpoint, state.PolicySetId.ValueString(), state.ID.ValueString())
	readReq, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating policy set parameter read request", fmt.Sprintf("Error: %s", err))
		return
	}
	readReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	readReq.Header.Add("Content-Type", "application/vnd.api+json")

	readResp, err := r.client.Do(readReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing policy set parameter read request", fmt.Sprintf("Error: %s", err))
		return
	}
	defer readResp.Body.Close()

	if readResp.StatusCode == http.StatusNotFound {
		resp.State.RemoveResource(ctx)
		return
	}

	bodyResponse, err := io.ReadAll(readResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading policy set parameter response body", fmt.Sprintf("Error: %s", err))
		return
	}

	if readResp.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error reading policy set parameter", fmt.Sprintf("status: %v, body: %v", readResp.Status, string(bodyResponse)))
		return
	}

	param := &client.PolicySetParameterEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), param); err != nil {
		resp.Diagnostics.AddError("Error unmarshalling policy set parameter payload", fmt.Sprintf("Error: %s", err))
		return
	}

	state.ID = types.StringValue(param.ID)
	state.Key = types.StringValue(param.Key)
	state.Value = types.StringValue(param.Value)
	if param.Description != nil {
		state.Description = types.StringValue(*param.Description)
	} else {
		state.Description = types.StringNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *PolicySetParameterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan PolicySetParameterResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.PolicySetParameterEntity{
		ID:    plan.ID.ValueString(),
		Key:   plan.Key.ValueString(),
		Value: plan.Value.ValueString(),
	}

	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		desc := plan.Description.ValueString()
		bodyRequest.Description = &desc
	}

	var out bytes.Buffer
	if err := jsonapi.MarshalPayload(&out, bodyRequest); err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	url := fmt.Sprintf("%s/api/v1/policy_set/%s/parameters/%s", r.endpoint, plan.PolicySetId.ValueString(), plan.ID.ValueString())
	updateReq, err := http.NewRequest(http.MethodPatch, url, strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating policy set parameter update request", fmt.Sprintf("Error: %s", err))
		return
	}
	updateReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	updateReq.Header.Add("Content-Type", "application/vnd.api+json")

	updateResp, err := r.client.Do(updateReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing policy set parameter update request", fmt.Sprintf("Error: %s", err))
		return
	}
	defer updateResp.Body.Close()

	bodyResponse, err := io.ReadAll(updateResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading policy set parameter response body", fmt.Sprintf("Error: %s", err))
		return
	}

	if updateResp.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error updating policy set parameter", fmt.Sprintf("status: %v, body: %v", updateResp.Status, string(bodyResponse)))
		return
	}

	updated := &client.PolicySetParameterEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), updated); err == nil && updated.ID != "" {
		plan.Key = types.StringValue(updated.Key)
		plan.Value = types.StringValue(updated.Value)
		if updated.Description != nil {
			plan.Description = types.StringValue(*updated.Description)
		} else {
			plan.Description = types.StringNull()
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PolicySetParameterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PolicySetParameterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	url := fmt.Sprintf("%s/api/v1/policy_set/%s/parameters/%s", r.endpoint, state.PolicySetId.ValueString(), state.ID.ValueString())
	delReq, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating policy set parameter delete request", fmt.Sprintf("Error: %s", err))
		return
	}
	delReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))

	delResp, err := r.client.Do(delReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing policy set parameter delete request", fmt.Sprintf("Error: %s", err))
		return
	}
	defer delResp.Body.Close()

	if delResp.StatusCode >= http.StatusBadRequest && delResp.StatusCode != http.StatusNotFound {
		bodyResponse, _ := io.ReadAll(delResp.Body)
		resp.Diagnostics.AddError("Error deleting policy set parameter", fmt.Sprintf("status: %v, body: %v", delResp.Status, string(bodyResponse)))
		return
	}

	tflog.Info(ctx, "Policy set parameter deleted", map[string]any{"id": state.ID.ValueString()})
}

func (r *PolicySetParameterResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var idParts []string
	if strings.Contains(req.ID, "/") {
		idParts = strings.Split(req.ID, "/")
	} else {
		idParts = strings.Split(req.ID, ",")
	}

	if len(idParts) != 2 || idParts[0] == "" || idParts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format 'policy_set_id/id' or 'policy_set_id,id', Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("policy_set_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[1])...)
}
