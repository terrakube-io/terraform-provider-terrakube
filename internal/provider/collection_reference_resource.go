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
var _ resource.Resource = &CollectionReferenceResource{}
var _ resource.ResourceWithImportState = &CollectionReferenceResource{}

type CollectionReferenceResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type CollectionReferenceResourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	CollectionId   types.String `tfsdk:"collection_id"`
	WorkspaceId    types.String `tfsdk:"workspace_id"`
	Description    types.String `tfsdk:"description"`
}

func NewCollectionReferenceResource() resource.Resource {
	return &CollectionReferenceResource{}
}

func (r *CollectionReferenceResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_collection_reference"
}

func (r *CollectionReferenceResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create collection reference that will be used by this workspace only.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Reference Id",
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
			"collection_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube collection id",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Variable description",
			},
		},
	}
}

func (r *CollectionReferenceResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Collection Item Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Collection reference resource", map[string]any{"success": true})
}

func (r *CollectionReferenceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan CollectionReferenceResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.CollectionReferenceEntity{
		Description: plan.Description.ValueStringPointer(),
		Workspace:   &client.WorkspaceEntity{ID: plan.WorkspaceId.ValueString()},
		Collection:  &client.CollectionEntity{ID: plan.CollectionId.ValueString()},
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	collectionReferenceRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/organization/%s/collection/%s/reference", r.endpoint, plan.OrganizationId.ValueString(), plan.CollectionId.ValueString()), strings.NewReader(out.String()))
	collectionReferenceRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	collectionReferenceRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating collection reference resource request", fmt.Sprintf("Error creating collection reference resource request: %s", err))
		return
	}

	collectionReferenceResponse, err := r.client.Do(collectionReferenceRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing collection reference resource request", fmt.Sprintf("Error executing collection reference resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(collectionReferenceResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading collection reference resource response")
	}
	collectionReference := &client.CollectionReferenceEntity{}

	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), collectionReference)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	plan.CollectionId = types.StringValue(collectionReference.Collection.ID)
	plan.WorkspaceId = types.StringValue(collectionReference.Workspace.ID)
	plan.Description = types.StringPointerValue(collectionReference.Description)
	plan.ID = types.StringValue(collectionReference.ID)

	tflog.Info(ctx, "collection reference Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *CollectionReferenceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state CollectionReferenceResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	collectionItemRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/reference/%s", r.endpoint, state.ID.ValueString()), nil)
	collectionItemRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	collectionItemRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating collection reference resource request", fmt.Sprintf("Error creating collection reference resource request: %s", err))
		return
	}

	collectionReferenceResponse, err := r.client.Do(collectionItemRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing collection reference resource request", fmt.Sprintf("Error executing collection reference resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(collectionReferenceResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading collection item resource response")
	}
	collectionReference := &client.CollectionReferenceEntity{}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), collectionReference)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	state.WorkspaceId = types.StringValue(collectionReference.Workspace.ID)
	state.CollectionId = types.StringValue(collectionReference.Collection.ID)
	state.Description = types.StringPointerValue(collectionReference.Description)
	state.ID = types.StringValue(collectionReference.ID)

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Collection reference Resource reading", map[string]any{"success": true})
}

func (r *CollectionReferenceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan CollectionReferenceResourceModel
	var state CollectionReferenceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.CollectionReferenceEntity{
		Description: plan.Description.ValueStringPointer(),
		Workspace:   &client.WorkspaceEntity{ID: plan.WorkspaceId.ValueString()},
		Collection:  &client.CollectionEntity{ID: plan.CollectionId.ValueString()},
		ID:          state.ID.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	collectionReferenceReq, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/reference/%s", r.endpoint, state.ID.ValueString()), strings.NewReader(out.String()))
	collectionReferenceReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	collectionReferenceReq.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating collection reference resource request", fmt.Sprintf("Error creating collection reference resource request: %s", err))
		return
	}

	collectionReferenceResponse, err := r.client.Do(collectionReferenceReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing collection reference resource request", fmt.Sprintf("Error executing collection reference resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(collectionReferenceResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading collection item resource response")
	}

	tflog.Info(ctx, "Body Response", map[string]any{"success": string(bodyResponse)})

	collectionReferenceReq, err = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/reference/%s", r.endpoint, state.ID.ValueString()), nil)
	collectionReferenceReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	collectionReferenceReq.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating collection reference resource request", fmt.Sprintf("Error creating collection reference resource request: %s", err))
		return
	}

	collectionReferenceResponse, err = r.client.Do(collectionReferenceReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing collection reference resource request", fmt.Sprintf("Error executing collection reference resource request: %s", err))
		return
	}

	bodyResponse, err = io.ReadAll(collectionReferenceResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading collection reference resource response body", fmt.Sprintf("Error reading collection reference resource response body: %s", err))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	collectionReference := &client.CollectionReferenceEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), collectionReference)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.Description = types.StringPointerValue(collectionReference.Description)
	plan.WorkspaceId = types.StringValue(collectionReference.Workspace.ID)
	plan.CollectionId = types.StringValue(collectionReference.Collection.ID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *CollectionReferenceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data CollectionReferenceResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	workspaceRequest, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/reference/%s", r.endpoint, data.ID.ValueString()), nil)
	workspaceRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	if err != nil {
		resp.Diagnostics.AddError("Error creating collection reference resource request", fmt.Sprintf("Error creating collection reference resource request: %s", err))
		return
	}

	_, err = r.client.Do(workspaceRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing collection reference resource request", fmt.Sprintf("Error executing collection reference resource request: %s", err))
		return
	}
}

func (r *CollectionReferenceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) != 3 || idParts[0] == "" || idParts[1] == "" || idParts[2] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: 'organization_ID,collection_ID, ID', Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("collection_id"), idParts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), idParts[2])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[3])...)
}
