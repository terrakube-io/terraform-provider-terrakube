package provider

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
var _ resource.Resource = &OrganizationVariableResource{}
var _ resource.ResourceWithImportState = &OrganizationVariableResource{}

type OrganizationVariableResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type OrganizationVariableResourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Key            types.String `tfsdk:"key"`
	Value          types.String `tfsdk:"value"`
	Description    types.String `tfsdk:"description"`
	Category       types.String `tfsdk:"category"`
	Sensitive      types.Bool   `tfsdk:"sensitive"`
	Hcl            types.Bool   `tfsdk:"hcl"`
}

func NewOrganizationVariableResource() resource.Resource {
	return &OrganizationVariableResource{}
}

func (r *OrganizationVariableResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_variable"
}

func (r *OrganizationVariableResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create an organization variable that can be used by all workspaces inside the organization.",

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

func (r *OrganizationVariableResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Organization Variable Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Organization Variable resource", map[string]any{"success": true})
}

func (r *OrganizationVariableResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan OrganizationVariableResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.OrganizationVariableEntity{
		Key:         plan.Key.ValueString(),
		Value:       plan.Value.ValueString(),
		Description: plan.Description.ValueString(),
		Sensitive:   plan.Sensitive.ValueBoolPointer(),
		Category:    plan.Category.ValueString(),
		Hcl:         plan.Hcl.ValueBool(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	organizationVarRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/organization/%s/globalvar", r.endpoint, plan.OrganizationId.ValueString()), strings.NewReader(out.String()))
	organizationVarRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	organizationVarRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating organization variable resource request", fmt.Sprintf("Error creating organization variable resource request: %s", err))
		return
	}

	organizationVarResponse, err := r.client.Do(organizationVarRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing organization variable  resource request", fmt.Sprintf("Error executing organization variable  resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(organizationVarResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading organization variable  resource response")
	}
	organizationVariable := &client.OrganizationVariableEntity{}

	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), organizationVariable)
	tflog.Info(ctx, string(bodyResponse))
	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	if *organizationVariable.Sensitive {
		tflog.Info(ctx, "Variable value is not included in response, setting values the same as the plan for sensitive=true...")
		plan.Value = types.StringValue(plan.Value.ValueString())
	} else {
		tflog.Info(ctx, "Variable value is included in response...")
		plan.Value = types.StringValue(organizationVariable.Value)
	}

	plan.Key = types.StringValue(organizationVariable.Key)
	plan.Description = types.StringValue(organizationVariable.Description)
	plan.Category = types.StringValue(organizationVariable.Category)
	plan.Sensitive = types.BoolValue(*organizationVariable.Sensitive)
	plan.Hcl = types.BoolValue(organizationVariable.Hcl)
	plan.ID = types.StringValue(organizationVariable.ID)

	tflog.Info(ctx, "organization variable Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *OrganizationVariableResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state OrganizationVariableResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	organizationVarRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/globalvar/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	organizationVarRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	organizationVarRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating organization variable resource request", fmt.Sprintf("Error creating organization variable resource request: %s", err))
		return
	}

	organizationVariableResponse, err := r.client.Do(organizationVarRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing organization variable resource request", fmt.Sprintf("Error executing organization variable resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(organizationVariableResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading organization variable resource response")
	}
	organizationVariable := &client.OrganizationVariableEntity{}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), organizationVariable)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	if *organizationVariable.Sensitive {
		tflog.Info(ctx, "Variable value is not included in response, setting values the same as the current state value")
		state.Value = types.StringValue(state.Value.ValueString())
	} else {
		tflog.Info(ctx, "Variable value is included in response...")
		state.Value = types.StringValue(organizationVariable.Value)
	}

	state.Key = types.StringValue(organizationVariable.Key)
	state.Description = types.StringValue(organizationVariable.Description)
	state.Category = types.StringValue(organizationVariable.Category)
	state.Sensitive = types.BoolValue(*organizationVariable.Sensitive)
	state.Hcl = types.BoolValue(organizationVariable.Hcl)
	state.ID = types.StringValue(organizationVariable.ID)

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "organization variable Resource reading", map[string]any{"success": true})
}

func (r *OrganizationVariableResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan OrganizationVariableResourceModel
	var state OrganizationVariableResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.OrganizationVariableEntity{
		Key:         plan.Key.ValueString(),
		Value:       plan.Value.ValueString(),
		Description: plan.Description.ValueString(),
		Category:    plan.Category.ValueString(),
		Hcl:         plan.Hcl.ValueBool(),
		ID:          state.ID.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	tflog.Info(ctx, "Body Update Request: "+out.String())

	organizationVarRequest, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/globalvar/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), strings.NewReader(out.String()))
	organizationVarRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	organizationVarRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating organization variable resource request", fmt.Sprintf("Error creating organization variable resource request: %s", err))
		return
	}

	organizationVarResponse, err := r.client.Do(organizationVarRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing organization variable resource request", fmt.Sprintf("Error executing organization variable resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(organizationVarResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading organization variable resource response")
	}

	tflog.Info(ctx, "Body Response", map[string]any{"success": string(bodyResponse)})

	organizationVarRequest, err = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/globalvar/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	organizationVarRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	organizationVarRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating organization variable resource request", fmt.Sprintf("Error creating organization variable resource request: %s", err))
		return
	}

	organizationVarResponse, err = r.client.Do(organizationVarRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing organization variable resource request", fmt.Sprintf("Error executing organization variable resource request: %s", err))
		return
	}

	bodyResponse, err = io.ReadAll(organizationVarResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading organization variable resource response body", fmt.Sprintf("Error reading organization variable resource response body: %s", err))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})
	tflog.Info(ctx, "Status"+strconv.Itoa(organizationVarResponse.StatusCode))
	organizationVariable := &client.OrganizationVariableEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), organizationVariable)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.Key = types.StringValue(organizationVariable.Key)

	if *organizationVariable.Sensitive {
		tflog.Info(ctx, "Variable value is not included in response, setting values the same as the plan for sensitive=true...")
		plan.Value = types.StringValue(plan.Value.ValueString())
	} else {
		tflog.Info(ctx, "Variable value is included in response...")
		plan.Value = types.StringValue(organizationVariable.Value)
	}

	plan.Description = types.StringValue(organizationVariable.Description)
	plan.Category = types.StringValue(organizationVariable.Category)
	plan.Sensitive = types.BoolValue(*organizationVariable.Sensitive)
	plan.Hcl = types.BoolValue(organizationVariable.Hcl)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *OrganizationVariableResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data OrganizationVariableResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	organizationVarRequest, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/organization/%s/globalvar/%s", r.endpoint, data.OrganizationId.ValueString(), data.ID.ValueString()), nil)
	organizationVarRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	if err != nil {
		resp.Diagnostics.AddError("Error creating organization variable resource request", fmt.Sprintf("Error creating organization variable resource request: %s", err))
		return
	}

	_, err = r.client.Do(organizationVarRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing organization variable resource request", fmt.Sprintf("Error executing organization variable resource request: %s", err))
		return
	}
}

func (r *OrganizationVariableResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
