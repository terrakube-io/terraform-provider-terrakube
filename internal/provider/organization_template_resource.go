package provider

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
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
var _ resource.Resource = &OrganizationTemplateResource{}
var _ resource.ResourceWithImportState = &OrganizationTemplateResource{}

type OrganizationTemplateResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type OrganizationTemplateResourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	Version        types.String `tfsdk:"version"`
	Content        types.String `tfsdk:"content"`
}

func NewOrganizationTemplateResource() resource.Resource {
	return &OrganizationTemplateResource{}
}

func (r *OrganizationTemplateResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_template"
}

func (r *OrganizationTemplateResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a template and attach it to your organization. This can be useful to define new" +
			"process like drift detection, cost mapping, etc...",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Template Id",
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
				Description: "The name of the template",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "The description of the template",
			},
			"version": schema.StringAttribute{
				Optional:    true,
				Description: "The version of the template",
			},
			"content": schema.StringAttribute{
				Required:    true,
				Description: "The content of the template",
			},
		},
	}
}

func (r *OrganizationTemplateResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Organization Template Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Organization Template resource", map[string]any{"success": true})
}

func (r *OrganizationTemplateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan OrganizationTemplateResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.OrganizationTemplateEntity{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueStringPointer(),
		Version:     plan.Version.ValueStringPointer(),
		Content:     base64.StdEncoding.EncodeToString([]byte(plan.Content.ValueString())),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	organizationTemplateRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/organization/%s/template", r.endpoint, plan.OrganizationId.ValueString()), strings.NewReader(out.String()))
	organizationTemplateRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	organizationTemplateRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating organization template resource request", fmt.Sprintf("Error creating organization template resource request: %s", err))
		return
	}

	organizationTemplateResponse, err := r.client.Do(organizationTemplateRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing organization template resource request", fmt.Sprintf("Error executing organization template resource request, response status: %s, response body: %s, error: %s", organizationTemplateResponse.Status, organizationTemplateResponse.Body, err))
		return
	}

	bodyResponse, err := io.ReadAll(organizationTemplateResponse.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading organization template resource response, response status: %s, response body: %s, error: %s", organizationTemplateResponse.Status, organizationTemplateResponse.Body, err))
	}
	organizationTemplate := &client.OrganizationTemplateEntity{}

	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), organizationTemplate)
	tflog.Info(ctx, string(bodyResponse))
	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response, response status: %s, response body: %s, error: %s", organizationTemplateResponse.Status, organizationTemplateResponse.Body, err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	plan.ID = types.StringValue(organizationTemplate.ID)
	plan.Name = types.StringValue(organizationTemplate.Name)
	plan.Description = types.StringPointerValue(organizationTemplate.Description)
	plan.Version = types.StringPointerValue(organizationTemplate.Version)
	contentDecoded, err := base64.StdEncoding.DecodeString(organizationTemplate.Content)
	if err != nil {
		resp.Diagnostics.AddError("Error decoding the content from Base64.", fmt.Sprintf("Error decode the tcl: %s", err))
		return
	}
	plan.Content = types.StringValue(string(contentDecoded))

	tflog.Info(ctx, "Organization Template Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *OrganizationTemplateResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state OrganizationTemplateResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	organizationTemplateRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/template/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	organizationTemplateRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	organizationTemplateRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating organization template resource request", fmt.Sprintf("Error creating organization template resource request: %s", err))
		return
	}

	organizationTemplateResponse, err := r.client.Do(organizationTemplateRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing organization template resource request", fmt.Sprintf("Error executing organization template resource request, response status: %s, response body: %s, error: %s", organizationTemplateResponse.Status, organizationTemplateResponse.Body, err))
		return
	}

	bodyResponse, err := io.ReadAll(organizationTemplateResponse.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading organization template resource response, response status: %s, response body: %s, error: %s", organizationTemplateResponse.Status, organizationTemplateResponse.Body, err))
	}
	organizationTemplate := &client.OrganizationTemplateEntity{}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), organizationTemplate)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	state.Name = types.StringValue(organizationTemplate.Name)
	state.Description = types.StringPointerValue(organizationTemplate.Description)
	state.Version = types.StringPointerValue(organizationTemplate.Version)
	contentDecoded, err := base64.StdEncoding.DecodeString(organizationTemplate.Content)
	if err != nil {
		resp.Diagnostics.AddError("Error decoding the content from Base64.", fmt.Sprintf("Error decode the tcl: %s", err))
		return
	}
	state.Content = types.StringValue(string(contentDecoded))
	state.ID = types.StringValue(organizationTemplate.ID)

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "organization template Resource reading", map[string]any{"success": true})
}

func (r *OrganizationTemplateResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan OrganizationTemplateResourceModel
	var state OrganizationTemplateResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.OrganizationTemplateEntity{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueStringPointer(),
		Version:     plan.Version.ValueStringPointer(),
		Content:     base64.StdEncoding.EncodeToString([]byte(plan.Content.ValueString())),
		ID:          state.ID.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	tflog.Info(ctx, "Body Update Request: "+out.String())

	organizationTemplateRequest, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/template/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), strings.NewReader(out.String()))
	organizationTemplateRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	organizationTemplateRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating organization template resource request", fmt.Sprintf("Error creating organization template resource request: %s", err))
		return
	}

	organizationTemplateResponse, err := r.client.Do(organizationTemplateRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing organization template resource request", fmt.Sprintf("Error executing organization template resource request, response status: %s, response body: %s, error: %s", organizationTemplateResponse.Status, organizationTemplateResponse.Body, err))
		return
	}

	bodyResponse, err := io.ReadAll(organizationTemplateResponse.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading organization template resource response, response status: %s, response body: %s, error: %s", organizationTemplateResponse.Status, organizationTemplateResponse.Body, err))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"success": string(bodyResponse)})

	organizationTemplateRequest, err = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/template/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	organizationTemplateRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	organizationTemplateRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating organization template resource request", fmt.Sprintf("Error creating organization template resource request: %s", err))
		return
	}

	organizationTemplateResponse, err = r.client.Do(organizationTemplateRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing organization template resource request", fmt.Sprintf("Error executing organization template resource request, response status: %s, response body: %s, error: %s", organizationTemplateResponse.Status, organizationTemplateResponse.Body, err))
		return
	}

	bodyResponse, err = io.ReadAll(organizationTemplateResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading organization template resource response body", fmt.Sprintf("Error reading organization template resource response body, response status: %s, response body: %s, error: %s", organizationTemplateResponse.Status, organizationTemplateResponse.Body, err))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})
	tflog.Info(ctx, "Status"+strconv.Itoa(organizationTemplateResponse.StatusCode))
	organizationTemplate := &client.OrganizationTemplateEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), organizationTemplate)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.Name = types.StringValue(organizationTemplate.Name)
	plan.Description = types.StringPointerValue(organizationTemplate.Description)
	plan.Version = types.StringPointerValue(organizationTemplate.Version)
	contentDecoded, err := base64.StdEncoding.DecodeString(organizationTemplate.Content)
	if err != nil {
		resp.Diagnostics.AddError("Error decoding the content from Base64.", fmt.Sprintf("Error decode the tcl: %s", err))
		return
	}
	plan.Content = types.StringValue(string(contentDecoded))

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *OrganizationTemplateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data OrganizationTemplateResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	organizationTemplateRequest, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/organization/%s/template/%s", r.endpoint, data.OrganizationId.ValueString(), data.ID.ValueString()), nil)
	organizationTemplateRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	if err != nil {
		resp.Diagnostics.AddError("Error creating organization template resource request", fmt.Sprintf("Error creating organization template resource request: %s", err))
		return
	}

	organizationTemplateResponse, err := r.client.Do(organizationTemplateRequest)
	if err != nil || organizationTemplateResponse.StatusCode != http.StatusNoContent {
		resp.Diagnostics.AddError("Error executing organization template resource request", fmt.Sprintf("Error executing organization template resource request, response status: %s, response body: %s, error: %s", organizationTemplateResponse.Status, organizationTemplateResponse.Body, err))
		return
	}
}

func (r *OrganizationTemplateResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
