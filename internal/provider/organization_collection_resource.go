package provider

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int32planmodifier"
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
var _ resource.Resource = &CollectionResource{}
var _ resource.ResourceWithImportState = &CollectionResource{}

type CollectionResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type CollectionResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Description    types.String `tfsdk:"description"`
	Priority       types.Int32  `tfsdk:"priority"`
}

func NewCollectionResource() resource.Resource {
	return &CollectionResource{}
}

func (r *CollectionResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_collection"
}

func (r *CollectionResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a collection and bind it to an organization.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Collection Id",
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
				Description: "Collection name",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Collection description",
			},
			"priority": schema.Int32Attribute{
				Required:    true,
				Description: "Collection priority",
				PlanModifiers: []planmodifier.Int32{
					int32planmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *CollectionResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected collection Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Collection resource", map[string]any{"success": true})
}

func (r *CollectionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan CollectionResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.CollectionEntity{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueStringPointer(),
		Priority:    plan.Priority.ValueInt32(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": strings.NewReader(out.String())})

	collectionRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/organization/%s/collection", r.endpoint, plan.OrganizationId.ValueString()), strings.NewReader(out.String()))
	collectionRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	collectionRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating collection resource request", fmt.Sprintf("Error creating collection resource request: %s", err))
		return
	}

	collectionResponse, err := r.client.Do(collectionRequest)
	if err != nil {

		resp.Diagnostics.AddError("Error executing collection resource request", fmt.Sprintf("Error executing collection resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(collectionResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading collection resource response")
	}
	newCollection := &client.CollectionEntity{}

	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), newCollection)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	plan.ID = types.StringValue(newCollection.ID)
	plan.Name = types.StringValue(newCollection.Name)
	plan.Description = types.StringPointerValue(newCollection.Description)
	plan.Priority = types.Int32Value(newCollection.Priority)

	tflog.Info(ctx, "Collection Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *CollectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state CollectionResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	collectionRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/collection/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	collectionRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	collectionRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating collection resource request", fmt.Sprintf("Error creating collection resource request: %s", err))
		return
	}

	collectionResponse, err := r.client.Do(collectionRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing collection resource request", fmt.Sprintf("Error executing collection resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(collectionResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading collection resource response")
	}
	collection := &client.CollectionEntity{}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), collection)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	state.Name = types.StringValue(collection.Name)
	state.Description = types.StringPointerValue(collection.Description)
	state.Priority = types.Int32Value(collection.Priority)

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Collection Resource reading", map[string]any{"success": true})
}

func (r *CollectionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan CollectionResourceModel
	var state CollectionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.CollectionEntity{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueStringPointer(),
		Priority:    plan.Priority.ValueInt32(),
		ID:          state.ID.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	collectionRequest, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/collection/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), strings.NewReader(out.String()))
	collectionRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	collectionRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating collection resource request", fmt.Sprintf("Error creating collection resource request: %s", err))
		return
	}

	collectionResponse, err := r.client.Do(collectionRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing collection resource request", fmt.Sprintf("Error executing collection resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(collectionResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading collection resource response")
	}

	tflog.Info(ctx, "Body Response", map[string]any{"success": string(bodyResponse)})

	collectionRequest, err = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/collection/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	collectionRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	collectionRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating collection resource request", fmt.Sprintf("Error creating collection resource request: %s", err))
		return
	}

	collectionResponse, err = r.client.Do(collectionRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing collection resource request", fmt.Sprintf("Error executing collection resource request: %s", err))
		return
	}

	bodyResponse, err = io.ReadAll(collectionResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading collection resource response body", fmt.Sprintf("Error reading collection resource response body: %s", err))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	collection := &client.CollectionEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), collection)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.Name = types.StringValue(collection.Name)
	plan.Description = types.StringPointerValue(collection.Description)
	plan.Priority = types.Int32Value(collection.Priority)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *CollectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data CollectionResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	reqOrg, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/organization/%s/collection/%s", r.endpoint, data.OrganizationId.ValueString(), data.ID.ValueString()), nil)
	reqOrg.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	if err != nil {
		resp.Diagnostics.AddError("Error creating collection resource request", fmt.Sprintf("Error creating collection resource request: %s", err))
		return
	}

	_, err = r.client.Do(reqOrg)
	if err != nil {
		resp.Diagnostics.AddError("Error executing collection resource request", fmt.Sprintf("Error executing collection resource request: %s", err))
		return
	}
}

func (r *CollectionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
