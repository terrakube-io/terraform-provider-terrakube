package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"terraform-provider-terrakube/internal/client"
	"unicode"
	"unicode/utf8"

	"github.com/google/jsonapi"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &RegistryProviderVersionResource{}
	_ resource.ResourceWithConfigure   = &RegistryProviderVersionResource{}
	_ resource.ResourceWithImportState = &RegistryProviderVersionResource{}
)

type RegistryProviderVersionResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type RegistryProviderVersionResourceModel struct {
	ID                 types.String `tfsdk:"id"`
	OrganizationId     types.String `tfsdk:"organization_id"`
	ProviderId         types.String `tfsdk:"provider_id"`
	VersionNumber      types.String `tfsdk:"version_number"`
	Protocols          types.List   `tfsdk:"protocols"`
	Status             types.String `tfsdk:"status"`
	DeprecationMessage types.String `tfsdk:"deprecation_message"`
}

func NewRegistryProviderVersionResource() resource.Resource {
	return &RegistryProviderVersionResource{}
}

func (r *RegistryProviderVersionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_registry_provider_version"
}

func (r *RegistryProviderVersionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replaceOnChange := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A version of a provider that you host in the private registry. Add the downloads for it with `terrakube_registry_provider_implementation`. A provider mirrored from the public registry (`imported = true`) gets its versions from Terrakube, so this resource is for providers you build yourself.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Version ID",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization_id": schema.StringAttribute{
				Required:      true,
				Description:   "Terrakube organization ID",
				PlanModifiers: replaceOnChange,
			},
			"provider_id": schema.StringAttribute{
				Required:      true,
				Description:   "ID of the `terrakube_registry_provider` this is a version of",
				PlanModifiers: replaceOnChange,
			},
			"version_number": schema.StringAttribute{
				Required:      true,
				Description:   "Version, for example `1.2.3`. Changing it creates a new version.",
				PlanModifiers: replaceOnChange,
			},
			"protocols": schema.ListAttribute{
				Required:    true,
				ElementType: types.StringType,
				Description: "Plugin protocol versions the provider supports, for example `[\"6.0\"]`. Providers built with the Terraform plugin framework use `6.0`, and ones built with SDKv2 use `5.0`. The registry cannot serve a version without them.",
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
					listvalidator.ValueStringsAre(stringvalidator.RegexMatches(protocolPattern, "must be a protocol version such as 5.0 or 6.0")),
				},
			},
			"status": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "`active`, `deprecated` (still served, with a warning) or `removed` (no longer served). Terrakube 2.34.0+.",
				Validators:  []validator.String{stringvalidator.OneOf("active", "deprecated", "removed")},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"deprecation_message": schema.StringAttribute{
				Optional:    true,
				Description: "Shown with a deprecated or removed version, for example when it will go or what to upgrade to. One line of up to 1024 characters. Terrakube 2.34.0+.",
				Validators:  []validator.String{deprecationMessageValidator{}},
			},
		},
	}
}

// deprecationMessageValidator accepts only what Terrakube stores unchanged. It replaces control
// and format characters with a space, strips the ends, and stores an empty text as null, so any
// other text would come back different from the configuration.
type deprecationMessageValidator struct{}

func (deprecationMessageValidator) Description(context.Context) string {
	return "one line of up to 1024 characters, without control characters or surrounding spaces"
}

func (v deprecationMessageValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (deprecationMessageValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	value := req.ConfigValue.ValueString()

	var problem string
	switch {
	case value == "":
		problem = "must not be empty. Leave it out instead"
	case utf8.RuneCountInString(value) > 1024:
		problem = "must be at most 1024 characters"
	case strings.TrimSpace(value) != value:
		problem = "must not start or end with whitespace"
	case strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) >= 0:
		problem = "must be one line, without control or format characters such as a line break"
	}
	if problem != "" {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid deprecation message",
			fmt.Sprintf("The deprecation message %s. Terrakube would store it differently from what is configured.", problem))
	}
}

func (r *RegistryProviderVersionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Registry Provider Version Resource Configure Type",
			fmt.Sprintf("Expected *TerrakubeConnectionData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}
	r.client = newTerrakubeHTTPClient(providerData)
	r.endpoint = providerData.Endpoint
	r.token = providerData.Token
}

func (r *RegistryProviderVersionResource) versionURL(organizationID, providerID, id string) string {
	url := fmt.Sprintf("%s/api/v1/organization/%s/provider/%s/version", r.endpoint, organizationID, providerID)
	if id != "" {
		url += "/" + id
	}
	return url
}

func (m *RegistryProviderVersionResourceModel) protocolList(ctx context.Context) ([]string, error) {
	var protocols []string
	if diags := m.Protocols.ElementsAs(ctx, &protocols, false); diags.HasError() {
		return nil, fmt.Errorf("reading protocols: %v", diags)
	}
	return protocols, nil
}

// registryProviderVersionRequestBody builds the JSON:API body for a create or an update. Status and
// the deprecation message are only sent when they are set, because a Terrakube older than 2.34.0
// does not know them. A message removed from the configuration is sent empty, since a PATCH that
// leaves it out keeps the old one.
func registryProviderVersionRequestBody(ctx context.Context, plan, prior *RegistryProviderVersionResourceModel, id string) ([]byte, error) {
	protocols, err := plan.protocolList(ctx)
	if err != nil {
		return nil, err
	}
	entity := &client.ProviderVersionEntity{
		ID:            id,
		VersionNumber: plan.VersionNumber.ValueString(),
		Protocols:     strings.Join(protocols, ","),
	}
	if !plan.Status.IsNull() && !plan.Status.IsUnknown() {
		entity.Status = plan.Status.ValueStringPointer()
	}
	switch {
	case !plan.DeprecationMessage.IsNull() && !plan.DeprecationMessage.IsUnknown():
		entity.DeprecationMessage = plan.DeprecationMessage.ValueStringPointer()
	case prior != nil && !prior.DeprecationMessage.IsNull():
		empty := ""
		entity.DeprecationMessage = &empty
	}
	return marshalJSONAPI(entity)
}

func (m *RegistryProviderVersionResourceModel) setFromAPI(version *client.ProviderVersionEntity) {
	m.ID = types.StringValue(version.ID)
	m.VersionNumber = types.StringValue(version.VersionNumber)

	elements := []types.String{}
	for _, protocol := range strings.Split(version.Protocols, ",") {
		if protocol = strings.TrimSpace(protocol); protocol != "" {
			elements = append(elements, types.StringValue(protocol))
		}
	}
	values := make([]attrValue, len(elements))
	for i, e := range elements {
		values[i] = e
	}
	m.Protocols = types.ListValueMust(types.StringType, values)

	// A Terrakube older than 2.34.0 has no status, and every version is served.
	m.Status = types.StringValue("active")
	if version.Status != nil && *version.Status != "" {
		m.Status = types.StringValue(*version.Status)
	}
	m.DeprecationMessage = stringFromAPI(m.DeprecationMessage, version.DeprecationMessage)
}

func (r *RegistryProviderVersionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan RegistryProviderVersionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, err := registryProviderVersionRequestBody(ctx, &plan, nil, "")
	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}
	response, responseBody, err := doJSONAPIRequest(ctx, r.client, r.token, http.MethodPost,
		r.versionURL(plan.OrganizationId.ValueString(), plan.ProviderId.ValueString(), ""), body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating registry provider version", err.Error())
		return
	}
	if reportAPIErrorStatus(&resp.Diagnostics, "creating registry provider version", response, responseBody) {
		return
	}

	created := &client.ProviderVersionEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(string(responseBody)), created); err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response",
			fmt.Sprintf("Error unmarshal payload response, response status: %s, response body: %s, error: %s", response.Status, responseBody, err))
		return
	}

	plan.setFromAPI(created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *RegistryProviderVersionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state RegistryProviderVersionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	response, responseBody, err := doJSONAPIRequest(ctx, r.client, r.token, http.MethodGet,
		r.versionURL(state.OrganizationId.ValueString(), state.ProviderId.ValueString(), state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error reading registry provider version", err.Error())
		return
	}
	if response.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Registry provider version not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if reportAPIErrorStatus(&resp.Diagnostics, "reading registry provider version", response, responseBody) {
		return
	}

	version := &client.ProviderVersionEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(string(responseBody)), version); err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response",
			fmt.Sprintf("Error unmarshal payload response, response status: %s, response body: %s, error: %s", response.Status, responseBody, err))
		return
	}

	state.setFromAPI(version)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *RegistryProviderVersionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state RegistryProviderVersionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	url := r.versionURL(state.OrganizationId.ValueString(), state.ProviderId.ValueString(), state.ID.ValueString())
	body, err := registryProviderVersionRequestBody(ctx, &plan, &state, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}
	response, responseBody, err := doJSONAPIRequest(ctx, r.client, r.token, http.MethodPatch, url, body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating registry provider version", err.Error())
		return
	}
	if reportAPIErrorStatus(&resp.Diagnostics, "updating registry provider version", response, responseBody) {
		return
	}

	response, responseBody, err = doJSONAPIRequest(ctx, r.client, r.token, http.MethodGet, url, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error reading registry provider version after update", err.Error())
		return
	}
	if reportAPIErrorStatus(&resp.Diagnostics, "reading registry provider version after update", response, responseBody) {
		return
	}
	updated := &client.ProviderVersionEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(string(responseBody)), updated); err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.setFromAPI(updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *RegistryProviderVersionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state RegistryProviderVersionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		r.versionURL(state.OrganizationId.ValueString(), state.ProviderId.ValueString(), state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating registry provider version request", fmt.Sprintf("Error creating registry provider version request: %s", err))
		return
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))

	response, err := r.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Error executing registry provider version request", fmt.Sprintf("Error executing registry provider version request: %s", err))
		return
	}
	reportDeleteFailure(&resp.Diagnostics, "deleting registry provider version", response)
}

func (r *RegistryProviderVersionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")
	if len(idParts) != 3 || idParts[0] == "" || idParts[1] == "" || idParts[2] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: 'organization_ID,provider_ID,ID', Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("provider_id"), idParts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[2])...)
}
