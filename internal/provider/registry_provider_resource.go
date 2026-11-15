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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                     = &RegistryProviderResource{}
	_ resource.ResourceWithConfigure        = &RegistryProviderResource{}
	_ resource.ResourceWithImportState      = &RegistryProviderResource{}
	_ resource.ResourceWithConfigValidators = &RegistryProviderResource{}
)

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

func (r *RegistryProviderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_registry_provider"
}

func (r *RegistryProviderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A provider in the private registry of an organization. Set `imported` and `registry_namespace` to mirror a provider from the public registry, and Terrakube keeps its versions up to date.",
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
				Description: "Terrakube organization ID",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Provider name, for example `aws`. For an imported provider it is the name in the public registry.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Provider description",
			},
			"imported": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Mirror the provider from the public registry. Needs `registry_namespace`. Terrakube 2.30.0+.",
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"registry_namespace": schema.StringAttribute{
				Optional:    true,
				Description: "Namespace of the provider in the public registry, for example `hashicorp`. Only used when `imported` is true. Terrakube 2.30.0+.",
			},
		},
	}
}

// ConfigValidators checks that an imported provider has the namespace Terrakube mirrors it from,
// and that a namespace is not set on a provider that is not imported.
func (r *RegistryProviderResource) ConfigValidators(context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{registryProviderImportValidator{}}
}

type registryProviderImportValidator struct{}

func (registryProviderImportValidator) Description(context.Context) string {
	return "registry_namespace is required when imported is true, and only allowed then"
}

func (v registryProviderImportValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (registryProviderImportValidator) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var imported types.Bool
	var namespace types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("imported"), &imported)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("registry_namespace"), &namespace)...)
	if resp.Diagnostics.HasError() || imported.IsUnknown() || namespace.IsUnknown() {
		return
	}

	switch {
	case imported.ValueBool() && namespace.IsNull():
		resp.Diagnostics.AddAttributeError(path.Root("registry_namespace"), "Missing registry namespace",
			"registry_namespace is required when imported is true: Terrakube mirrors the provider from that namespace in the public registry.")
	case !imported.ValueBool() && !namespace.IsNull():
		resp.Diagnostics.AddAttributeError(path.Root("registry_namespace"), "Unexpected registry namespace",
			"registry_namespace is only used when imported is true.")
	}
}

func (r *RegistryProviderResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

	r.client = &http.Client{}
	if providerData.InsecureHttpClient {
		if custom, ok := http.DefaultTransport.(*http.Transport); ok {
			customTransport := custom.Clone()
			customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
			r.client = &http.Client{Transport: customTransport}
		}
	}
	r.endpoint = providerData.Endpoint
	r.token = providerData.Token
}

func (r *RegistryProviderResource) providerURL(organizationID string, id string) string {
	url := fmt.Sprintf("%s/api/v1/organization/%s/provider", r.endpoint, organizationID)
	if id != "" {
		url += "/" + id
	}
	return url
}

// do sends a request and returns the response with its body read and closed.
func (r *RegistryProviderResource) do(ctx context.Context, method, url string, body []byte) (*http.Response, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, nil, fmt.Errorf("creating request: %w", err)
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")

	response, err := r.client.Do(request)
	if err != nil {
		return nil, nil, fmt.Errorf("executing request: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return response, nil, fmt.Errorf("reading response: %w", err)
	}
	return response, responseBody, nil
}

// requestBody builds the JSON:API body for a create or an update. Imported and the namespace are
// only sent when they are set, because a Terrakube older than 2.30.0 does not know them.
func registryProviderRequestBody(plan, prior *RegistryProviderResourceModel, id string) ([]byte, error) {
	entity := &client.ProviderEntity{
		ID:          id,
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueStringPointer(),
	}
	if !plan.Imported.IsNull() && !plan.Imported.IsUnknown() {
		entity.Imported = plan.Imported.ValueBoolPointer()
	}
	switch {
	case !plan.RegistryNamespace.IsNull() && !plan.RegistryNamespace.IsUnknown():
		entity.RegistryNamespace = plan.RegistryNamespace.ValueStringPointer()
	case prior != nil && !prior.RegistryNamespace.IsNull():
		// Leaving the attribute out of a PATCH keeps the old value, so a removed namespace is sent empty.
		empty := ""
		entity.RegistryNamespace = &empty
	}

	var out bytes.Buffer
	if err := jsonapi.MarshalPayload(&out, entity); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// stringFromAPI keeps a null a null: Terrakube may return an empty string for an attribute that
// was never set, and Terraform would report a difference between the plan and the result.
func stringFromAPI(current types.String, value *string) types.String {
	if value == nil || (*value == "" && current.IsNull()) {
		return types.StringNull()
	}
	return types.StringValue(*value)
}

func (m *RegistryProviderResourceModel) setFromAPI(provider *client.ProviderEntity) {
	m.ID = types.StringValue(provider.ID)
	m.Name = types.StringValue(provider.Name)
	m.Description = stringFromAPI(m.Description, provider.Description)
	m.Imported = types.BoolValue(provider.Imported != nil && *provider.Imported)
	m.RegistryNamespace = stringFromAPI(m.RegistryNamespace, provider.RegistryNamespace)
}

func (r *RegistryProviderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan RegistryProviderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, err := registryProviderRequestBody(&plan, nil, "")
	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}
	response, responseBody, err := r.do(ctx, http.MethodPost, r.providerURL(plan.OrganizationId.ValueString(), ""), body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating registry provider", err.Error())
		return
	}
	if reportAPIErrorStatus(&resp.Diagnostics, "creating registry provider", response, responseBody) {
		return
	}

	created := &client.ProviderEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(string(responseBody)), created); err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response",
			fmt.Sprintf("Error unmarshal payload response, response status: %s, response body: %s, error: %s", response.Status, responseBody, err))
		return
	}

	plan.setFromAPI(created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *RegistryProviderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state RegistryProviderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	response, responseBody, err := r.do(ctx, http.MethodGet, r.providerURL(state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error reading registry provider", err.Error())
		return
	}
	if response.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Registry provider not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if reportAPIErrorStatus(&resp.Diagnostics, "reading registry provider", response, responseBody) {
		return
	}

	provider := &client.ProviderEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(string(responseBody)), provider); err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response",
			fmt.Sprintf("Error unmarshal payload response, response status: %s, response body: %s, error: %s", response.Status, responseBody, err))
		return
	}

	state.setFromAPI(provider)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *RegistryProviderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state RegistryProviderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	url := r.providerURL(state.OrganizationId.ValueString(), state.ID.ValueString())
	body, err := registryProviderRequestBody(&plan, &state, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}
	response, responseBody, err := r.do(ctx, http.MethodPatch, url, body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating registry provider", err.Error())
		return
	}
	if reportAPIErrorStatus(&resp.Diagnostics, "updating registry provider", response, responseBody) {
		return
	}

	response, responseBody, err = r.do(ctx, http.MethodGet, url, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error reading registry provider after update", err.Error())
		return
	}
	if reportAPIErrorStatus(&resp.Diagnostics, "reading registry provider after update", response, responseBody) {
		return
	}
	updated := &client.ProviderEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(string(responseBody)), updated); err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.setFromAPI(updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *RegistryProviderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state RegistryProviderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, r.providerURL(state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating registry provider request", fmt.Sprintf("Error creating registry provider request: %s", err))
		return
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))

	response, err := r.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Error executing registry provider request", fmt.Sprintf("Error executing registry provider request: %s", err))
		return
	}
	reportDeleteFailure(&resp.Diagnostics, "deleting registry provider", response)
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
