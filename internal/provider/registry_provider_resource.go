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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ resource.Resource = &RegistryProviderResource{}
var _ resource.ResourceWithImportState = &RegistryProviderResource{}

type RegistryProviderResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type RegistryProviderResourceModel struct {
	ID                types.String `tfsdk:"id"`
	OrganizationId    types.String `tfsdk:"organization_id"`
	Name              types.String `tfsdk:"name"`
	Description       types.String `tfsdk:"description"`
	Imported          types.Bool   `tfsdk:"imported"`
	RegistryNamespace types.String `tfsdk:"registry_namespace"`
}

func NewRegistryProviderResource() resource.Resource {
	return &RegistryProviderResource{}
}

func (r *RegistryProviderResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_registry_provider"
}

func (r *RegistryProviderResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Publishes a provider in the organization's private Terrakube registry. " +
			"Set `imported = true` together with `registry_namespace` (e.g. `hashicorp/random`) to mirror an upstream provider " +
			"— Terrakube will then schedule a refresh job to pull version implementations from the public registry. " +
			"For fully-private providers, leave `imported = false` and upload versions/implementations out-of-band.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Provider ID",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization ID that owns the registry",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Provider name (e.g. `random`, `aws`). Combined with the organization, this forms the registry source `{organization}/{name}`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Provider description",
			},
			"imported": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "When `true`, Terrakube treats this as a mirror of an upstream provider and uses `registry_namespace` to refresh versions from the public registry. Defaults to `false`.",
			},
			"registry_namespace": schema.StringAttribute{
				Optional:    true,
				Description: "Upstream namespace in the form `{namespace}` (e.g. `hashicorp`). Required when `imported = true`; ignored otherwise.",
			},
		},
	}
}

func (r *RegistryProviderResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Registry Provider Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Registry Provider resource", map[string]any{"success": true})
}

func (r *RegistryProviderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan RegistryProviderResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if plan.Imported.ValueBool() && plan.RegistryNamespace.IsNull() {
		resp.Diagnostics.AddAttributeError(
			path.Root("registry_namespace"),
			"Missing registry_namespace",
			"`registry_namespace` is required when `imported = true`. Use the upstream namespace, e.g. `hashicorp` for `hashicorp/random`.",
		)
		return
	}

	bodyRequest := &client.ProviderEntity{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
		Imported:    plan.Imported.ValueBool(),
	}
	if !plan.RegistryNamespace.IsNull() {
		bodyRequest.RegistryNamespace = plan.RegistryNamespace.ValueString()
	}

	var out = new(bytes.Buffer)
	if err := jsonapi.MarshalPayload(out, bodyRequest); err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	tflog.Info(ctx, fmt.Sprintf("Registry Provider Create Body: %s", out.String()))

	providerRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/organization/%s/provider", r.endpoint, plan.OrganizationId.ValueString()), strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating registry provider resource request", fmt.Sprintf("Error creating registry provider resource request: %s", err))
		return
	}
	providerRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	providerRequest.Header.Add("Content-Type", "application/vnd.api+json")

	providerResponse, err := r.client.Do(providerRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing registry provider resource request", fmt.Sprintf("Error executing registry provider resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(providerResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading registry provider resource response")
	}

	if providerResponse.StatusCode >= 400 {
		resp.Diagnostics.AddError("Registry provider creation failed", fmt.Sprintf("status: %s, body: %s", providerResponse.Status, string(bodyResponse)))
		return
	}

	tflog.Info(ctx, "Registry Provider Create Response", map[string]any{"bodyResponse": string(bodyResponse)})

	newProvider := &client.ProviderEntity{}
	if err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), newProvider); err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ID = types.StringValue(newProvider.ID)
	plan.Name = types.StringValue(newProvider.Name)
	if newProvider.Description != "" {
		plan.Description = types.StringValue(newProvider.Description)
	}
	plan.Imported = types.BoolValue(newProvider.Imported)
	if newProvider.RegistryNamespace != "" {
		plan.RegistryNamespace = types.StringValue(newProvider.RegistryNamespace)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *RegistryProviderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state RegistryProviderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	providerRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/provider/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating registry provider resource request", fmt.Sprintf("Error creating registry provider resource request: %s", err))
		return
	}
	providerRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	providerRequest.Header.Add("Content-Type", "application/vnd.api+json")

	providerResponse, err := r.client.Do(providerRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing registry provider resource request", fmt.Sprintf("Error executing registry provider resource request: %s", err))
		return
	}

	if providerResponse.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Registry provider not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	bodyResponse, err := io.ReadAll(providerResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading registry provider resource response")
	}

	provider := &client.ProviderEntity{}
	if err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), provider); err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	state.Name = types.StringValue(provider.Name)
	if provider.Description != "" {
		state.Description = types.StringValue(provider.Description)
	} else {
		state.Description = types.StringNull()
	}
	state.Imported = types.BoolValue(provider.Imported)
	if provider.RegistryNamespace != "" {
		state.RegistryNamespace = types.StringValue(provider.RegistryNamespace)
	} else {
		state.RegistryNamespace = types.StringNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *RegistryProviderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan RegistryProviderResourceModel
	var state RegistryProviderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.ProviderEntity{
		ID:          state.ID.ValueString(),
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
		Imported:    plan.Imported.ValueBool(),
	}
	if !plan.RegistryNamespace.IsNull() {
		bodyRequest.RegistryNamespace = plan.RegistryNamespace.ValueString()
	}

	var out = new(bytes.Buffer)
	if err := jsonapi.MarshalPayload(out, bodyRequest); err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	providerRequest, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/provider/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating registry provider resource request", fmt.Sprintf("Error creating registry provider resource request: %s", err))
		return
	}
	providerRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	providerRequest.Header.Add("Content-Type", "application/vnd.api+json")

	patchResponse, err := r.client.Do(providerRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing registry provider resource request", fmt.Sprintf("Error executing registry provider resource request: %s", err))
		return
	}
	patchBody, _ := io.ReadAll(patchResponse.Body)
	tflog.Info(ctx, "Registry Provider Update Response", map[string]any{"bodyResponse": string(patchBody)})

	if patchResponse.StatusCode >= 400 {
		resp.Diagnostics.AddError("Registry provider update failed", fmt.Sprintf("status: %s, body: %s", patchResponse.Status, string(patchBody)))
		return
	}

	getRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/provider/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating registry provider resource request", fmt.Sprintf("Error creating registry provider resource request: %s", err))
		return
	}
	getRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	getRequest.Header.Add("Content-Type", "application/vnd.api+json")

	getResponse, err := r.client.Do(getRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing registry provider resource request", fmt.Sprintf("Error executing registry provider resource request: %s", err))
		return
	}
	getBody, err := io.ReadAll(getResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading registry provider resource response body", fmt.Sprintf("Error reading registry provider resource response body: %s", err))
		return
	}

	updated := &client.ProviderEntity{}
	if err = jsonapi.UnmarshalPayload(strings.NewReader(string(getBody)), updated); err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.Name = types.StringValue(updated.Name)
	if updated.Description != "" {
		plan.Description = types.StringValue(updated.Description)
	} else {
		plan.Description = types.StringNull()
	}
	plan.Imported = types.BoolValue(updated.Imported)
	if updated.RegistryNamespace != "" {
		plan.RegistryNamespace = types.StringValue(updated.RegistryNamespace)
	} else {
		plan.RegistryNamespace = types.StringNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *RegistryProviderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data RegistryProviderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	delRequest, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/organization/%s/provider/%s", r.endpoint, data.OrganizationId.ValueString(), data.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating registry provider resource request", fmt.Sprintf("Error creating registry provider resource request: %s", err))
		return
	}
	delRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))

	if _, err = r.client.Do(delRequest); err != nil {
		resp.Diagnostics.AddError("Error executing registry provider resource request", fmt.Sprintf("Error executing registry provider resource request: %s", err))
		return
	}
}

func (r *RegistryProviderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
