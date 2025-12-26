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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &WorkspaceVariableResource{}
var _ resource.ResourceWithImportState = &WorkspaceVariableResource{}

type WorkspaceVariableResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type WorkspaceVariableResourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	WorkspaceId    types.String `tfsdk:"workspace_id"`
	Key            types.String `tfsdk:"key"`
	Value          types.String `tfsdk:"value"`
	Description    types.String `tfsdk:"description"`
	Category       types.String `tfsdk:"category"`
	Sensitive      types.Bool   `tfsdk:"sensitive"`
	Hcl            types.Bool   `tfsdk:"hcl"`
}

func NewWorkspaceVariableResource() resource.Resource {
	return &WorkspaceVariableResource{}
}

func (r *WorkspaceVariableResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_variable"
}

func (r *WorkspaceVariableResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create variables that will be used by this workspace only.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Variable Id",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization id",
			},
			"workspace_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube workspace id",
			},
			"key": schema.StringAttribute{
				Required:    true,
				Description: "Variable key",
			},
			"value": schema.StringAttribute{
				Required:    true,
				Description: "Variable value",
			},
			"description": schema.StringAttribute{
				Required:    true,
				Description: "Variable description",
			},
			"category": schema.StringAttribute{
				Required:    true,
				Description: "Variable category (ENV or TERRAFORM). ENV variables are injected in workspace environment at runtime.",
			},
			"sensitive": schema.BoolAttribute{
				Required:    true,
				Description: "Sensitive variables are never shown in the UI or API. They may appear in Terraform logs if your configuration is designed to output them.",
			},
			"hcl": schema.BoolAttribute{
				Required:    true,
				Description: "Parse this field as HashiCorp Configuration Language (HCL). This allows you to interpolate values at runtime.",
			},
		},
	}
}

func (r *WorkspaceVariableResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Workspace Variable Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Workspace Variable resource", map[string]any{"success": true})
}

func (r *WorkspaceVariableResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceVariableResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.WorkspaceVariableEntity{
		Key:         plan.Key.ValueString(),
		Value:       plan.Value.ValueString(),
		Description: plan.Description.ValueString(),
		Sensitive:   plan.Sensitive.ValueBool(),
		Category:    plan.Category.ValueString(),
		Hcl:         plan.Hcl.ValueBool(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	workspaceVarRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/variable", r.endpoint, plan.OrganizationId.ValueString(), plan.WorkspaceId.ValueString()), strings.NewReader(out.String()))
	workspaceVarRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceVarRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace variable resource request", fmt.Sprintf("Error creating workspace variable resource request: %s", err))
		return
	}

	workspaceVarResponse, err := r.client.Do(workspaceVarRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace variable  resource request", fmt.Sprintf("Error executing workspace variable  resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(workspaceVarResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading workspace variable  resource response")
	}
	workspaceVariable := &client.WorkspaceVariableEntity{}

	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), workspaceVariable)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	if workspaceVariable.Sensitive {
		tflog.Info(ctx, "Variable value is not included in response, setting values the same as the plan for sensitive=true...")
		plan.Value = types.StringValue(plan.Value.ValueString())
	} else {
		tflog.Info(ctx, "Variable value is included in response...")
		plan.Value = types.StringValue(workspaceVariable.Value)
	}

	plan.Key = types.StringValue(workspaceVariable.Key)
	plan.Description = types.StringValue(workspaceVariable.Description)
	plan.Category = types.StringValue(workspaceVariable.Category)
	plan.Sensitive = types.BoolValue(workspaceVariable.Sensitive)
	plan.Hcl = types.BoolValue(workspaceVariable.Hcl)
	plan.ID = types.StringValue(workspaceVariable.ID)

	tflog.Info(ctx, "workspace variable Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceVariableResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceVariableResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceVariableRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/variable/%s", r.endpoint, state.OrganizationId.ValueString(), state.WorkspaceId.ValueString(), state.ID.ValueString()), nil)
	workspaceVariableRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceVariableRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace variable resource request", fmt.Sprintf("Error creating workspace variable resource request: %s", err))
		return
	}

	workspaceVariableResponse, err := r.client.Do(workspaceVariableRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace variable resource request", fmt.Sprintf("Error executing workspace variable resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(workspaceVariableResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading workspace variable resource response")
	}
	workspaceVariable := &client.WorkspaceVariableEntity{}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), workspaceVariable)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	if workspaceVariable.Sensitive {
		tflog.Info(ctx, "Variable value is not included in response, setting values the same as the current state value")
		state.Value = types.StringValue(state.Value.ValueString())
	} else {
		tflog.Info(ctx, "Variable value is included in response...")
		state.Value = types.StringValue(workspaceVariable.Value)
	}

	state.Key = types.StringValue(workspaceVariable.Key)
	state.Description = types.StringValue(workspaceVariable.Description)
	state.Category = types.StringValue(workspaceVariable.Category)
	state.Sensitive = types.BoolValue(workspaceVariable.Sensitive)
	state.Hcl = types.BoolValue(workspaceVariable.Hcl)
	state.ID = types.StringValue(workspaceVariable.ID)

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Workspace variable Resource reading", map[string]any{"success": true})
}

func (r *WorkspaceVariableResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan WorkspaceVariableResourceModel
	var state WorkspaceVariableResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.WorkspaceVariableEntity{
		Key:         plan.Key.ValueString(),
		Value:       plan.Value.ValueString(),
		Description: plan.Description.ValueString(),
		Category:    plan.Category.ValueString(),
		Sensitive:   plan.Sensitive.ValueBool(),
		Hcl:         plan.Hcl.ValueBool(),
		ID:          state.ID.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	workspaceVariableReq, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/variable/%s", r.endpoint, state.OrganizationId.ValueString(), state.WorkspaceId.ValueString(), state.ID.ValueString()), strings.NewReader(out.String()))
	workspaceVariableReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceVariableReq.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating Workspace variable resource request", fmt.Sprintf("Error creating Workspace variable resource request: %s", err))
		return
	}

	workspaceVariableResponse, err := r.client.Do(workspaceVariableReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing Workspace variable resource request", fmt.Sprintf("Error executing Workspace variable resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(workspaceVariableResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading Workspace variable resource response")
	}

	tflog.Info(ctx, "Body Response", map[string]any{"success": string(bodyResponse)})

	workspaceVariableReq, err = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/variable/%s", r.endpoint, state.OrganizationId.ValueString(), state.WorkspaceId.ValueString(), state.ID.ValueString()), nil)
	workspaceVariableReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceVariableReq.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating Workspace variable resource request", fmt.Sprintf("Error creating Workspace variable resource request: %s", err))
		return
	}

	workspaceVariableResponse, err = r.client.Do(workspaceVariableReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing Workspace variable resource request", fmt.Sprintf("Error executing Workspace variable resource request: %s", err))
		return
	}

	bodyResponse, err = io.ReadAll(workspaceVariableResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading Workspace variable resource response body", fmt.Sprintf("Error reading Workspace variable resource response body: %s", err))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	workspaceVariable := &client.WorkspaceVariableEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), workspaceVariable)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	if workspaceVariable.Sensitive {
		tflog.Info(ctx, "Variable value is not included in response, setting values the same as the plan for sensitive=true...")
		plan.Value = types.StringValue(plan.Value.ValueString())
	} else {
		tflog.Info(ctx, "Variable value is included in response...")
		plan.Value = types.StringValue(workspaceVariable.Value)
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.Key = types.StringValue(workspaceVariable.Key)
	plan.Description = types.StringValue(workspaceVariable.Description)
	plan.Category = types.StringValue(workspaceVariable.Category)
	plan.Sensitive = types.BoolValue(workspaceVariable.Sensitive)
	plan.Hcl = types.BoolValue(workspaceVariable.Hcl)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceVariableResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data WorkspaceVariableResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	workspaceRequest, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/variable/%s", r.endpoint, data.OrganizationId.ValueString(), data.WorkspaceId.ValueString(), data.ID.ValueString()), nil)
	workspaceRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	if err != nil {
		resp.Diagnostics.AddError("Error creating Workspace variable resource request", fmt.Sprintf("Error creating Workspace variable resource request: %s", err))
		return
	}

	_, err = r.client.Do(workspaceRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing Workspace variable resource request", fmt.Sprintf("Error executing Workspace variable resource request: %s", err))
		return
	}
}

func (r *WorkspaceVariableResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) != 3 || idParts[0] == "" || idParts[1] == "" || idParts[2] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: 'organization_ID,workspace_ID, ID', Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), idParts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[2])...)
}
