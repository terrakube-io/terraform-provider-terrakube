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
var _ resource.Resource = &OrganizationTagResource{}
var _ resource.ResourceWithImportState = &OrganizationTagResource{}

type OrganizationTagResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type OrganizationTagResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	OrganizationId types.String `tfsdk:"organization_id"`
}

func NewOrganizationTagResource() resource.Resource {
	return &OrganizationTagResource{}
}

func (r *OrganizationTagResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_tag"
}

func (r *OrganizationTagResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a tag and attach it to your organization.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Organization Tag Id",
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
				Description: "Organization Tag name",
			},
		},
	}
}

func (r *OrganizationTagResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Organization Tag Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Organization Tag resource", map[string]any{"success": true})
}

func (r *OrganizationTagResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan OrganizationTagResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.OrganizationTagEntity{
		Name: plan.Name.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	organizationTagRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/organization/%s/tag", r.endpoint, plan.OrganizationId.ValueString()), strings.NewReader(out.String()))
	organizationTagRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	organizationTagRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating organization tag resource request", fmt.Sprintf("Error creating organization tag resource request: %s", err))
		return
	}

	organizationTagResponse, err := r.client.Do(organizationTagRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing organization tag resource request", fmt.Sprintf("Error executing organization tag resource request, response status: %s, response body: %s, body: %s", organizationTagResponse.Status, organizationTagResponse.Body, err))
		return
	}

	bodyResponse, err := io.ReadAll(organizationTagResponse.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading organization tag resource response, response status: %s, response body: %s, body: %s", organizationTagResponse.Status, organizationTagResponse.Body, err))
	}
	newOrganizationTag := &client.OrganizationTagEntity{}

	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), newOrganizationTag)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response, response status: %s, response body: %s, body: %s", organizationTagResponse.Status, organizationTagResponse.Body, err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	plan.ID = types.StringValue(newOrganizationTag.ID)
	plan.Name = types.StringValue(newOrganizationTag.Name)

	tflog.Info(ctx, "Organization Tag Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *OrganizationTagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state OrganizationTagResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	organizationTagRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/tag/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	organizationTagRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	organizationTagRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating organization tag resource request", fmt.Sprintf("Error creating organization tag resource request: %s", err))
		return
	}

	organizationTagResponse, err := r.client.Do(organizationTagRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing organization tag resource request", fmt.Sprintf("Error executing organization tag resource request, response status: %s, response body: %s, body: %s", organizationTagResponse.Status, organizationTagResponse.Body, err))
		return
	}

	if organizationTagResponse.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Organization tag not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	bodyResponse, err := io.ReadAll(organizationTagResponse.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading organization tag resource response, response status: %s, response body: %s, body: %s", organizationTagResponse.Status, organizationTagResponse.Body, err))
	}
	organizationTag := &client.OrganizationTagEntity{}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), organizationTag)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response, response status: %s, response body: %s, body: %s", organizationTagResponse.Status, organizationTagResponse.Body, err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	state.Name = types.StringValue(organizationTag.Name)

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Organization Tag Resource reading", map[string]any{"success": true})
}

func (r *OrganizationTagResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan OrganizationTagResourceModel
	var state OrganizationTagResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.OrganizationTagEntity{
		ID:   plan.ID.ValueString(),
		Name: plan.Name.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	organizationTagRequest, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/tag/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), strings.NewReader(out.String()))
	organizationTagRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	organizationTagRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating organization tag resource request", fmt.Sprintf("Error creating organization tag resource request: %s", err))
		return
	}

	organizationTagResponse, err := r.client.Do(organizationTagRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing organization tag resource request", fmt.Sprintf("Error executing organization tag resource request, response status: %s, response body: %s, body: %s", organizationTagResponse.Status, organizationTagResponse.Body, err))
		return
	}

	bodyResponse, err := io.ReadAll(organizationTagResponse.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading organization tag resource response, response status: %s, response body: %s, body: %s", organizationTagResponse.Status, organizationTagResponse.Body, err))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"success": string(bodyResponse)})

	organizationTagRequest, err = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/tag/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	organizationTagRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	organizationTagRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating organization tag resource request", fmt.Sprintf("Error creating organization tag resource request: %s", err))
		return
	}

	organizationTagResponse, err = r.client.Do(organizationTagRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing organization tag resource request", fmt.Sprintf("Error executing organization tag resource request, response status: %s, response body: %s, body: %s", organizationTagResponse.Status, organizationTagResponse.Body, err))
		return
	}

	bodyResponse, err = io.ReadAll(organizationTagResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading organization tag resource response body", fmt.Sprintf("Error reading organization tag resource response body, response status: %s, response body: %s, body: %s", organizationTagResponse.Status, organizationTagResponse.Body, err))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	organizationTag := &client.OrganizationTagEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), organizationTag)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ID = types.StringValue(organizationTag.ID)
	plan.Name = types.StringValue(organizationTag.Name)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *OrganizationTagResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data OrganizationTagResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	reqOrg, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/organization/%s/tag/%s", r.endpoint, data.OrganizationId.ValueString(), data.ID.ValueString()), nil)
	reqOrg.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	if err != nil {
		resp.Diagnostics.AddError("Error creating organization tag resource request", fmt.Sprintf("Error creating organization tag resource request: %s", err))
		return
	}

	organizationTagResponse, err := r.client.Do(reqOrg)
	if err != nil || organizationTagResponse.StatusCode != http.StatusNoContent {
		resp.Diagnostics.AddError("Error executing organization tag resource request", fmt.Sprintf("Error executing organization tag resource request, response status: %s, response body: %s, body: %s", organizationTagResponse.Status, organizationTagResponse.Body, err))
		return
	}
}

func (r *OrganizationTagResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
