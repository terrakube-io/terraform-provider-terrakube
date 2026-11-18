package provider

import (
	"context"
	"fmt"
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

var (
	_ resource.Resource                = &RegistryProviderImplementationResource{}
	_ resource.ResourceWithConfigure   = &RegistryProviderImplementationResource{}
	_ resource.ResourceWithImportState = &RegistryProviderImplementationResource{}
)

type RegistryProviderImplementationResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type RegistryProviderImplementationResourceModel struct {
	ID                  types.String `tfsdk:"id"`
	OrganizationId      types.String `tfsdk:"organization_id"`
	ProviderId          types.String `tfsdk:"provider_id"`
	VersionId           types.String `tfsdk:"version_id"`
	Os                  types.String `tfsdk:"os"`
	Arch                types.String `tfsdk:"arch"`
	Filename            types.String `tfsdk:"filename"`
	DownloadUrl         types.String `tfsdk:"download_url"`
	ShasumsUrl          types.String `tfsdk:"shasums_url"`
	ShasumsSignatureUrl types.String `tfsdk:"shasums_signature_url"`
	Shasum              types.String `tfsdk:"shasum"`
	KeyId               types.String `tfsdk:"key_id"`
	AsciiArmor          types.String `tfsdk:"ascii_armor"`
	TrustSignature      types.String `tfsdk:"trust_signature"`
	Source              types.String `tfsdk:"source"`
	SourceUrl           types.String `tfsdk:"source_url"`
}

func NewRegistryProviderImplementationResource() resource.Resource {
	return &RegistryProviderImplementationResource{}
}

func (r *RegistryProviderImplementationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_registry_provider_implementation"
}

func (r *RegistryProviderImplementationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replaceOnChange := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	required := func(description string) schema.StringAttribute {
		return schema.StringAttribute{Required: true, Description: description}
	}
	optional := func(description string) schema.StringAttribute {
		return schema.StringAttribute{Optional: true, Description: description}
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "The download of a version of a provider you host in the private registry, for one operating system and architecture. Terraform and OpenTofu are pointed at `download_url` and check the file against `shasum`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Implementation ID",
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
				Description:   "ID of the `terrakube_registry_provider`",
				PlanModifiers: replaceOnChange,
			},
			"version_id": schema.StringAttribute{
				Required:      true,
				Description:   "ID of the `terrakube_registry_provider_version` this is a download of",
				PlanModifiers: replaceOnChange,
			},
			"os":                    required("Operating system, for example `linux`, `darwin` or `windows`"),
			"arch":                  required("Architecture, for example `amd64` or `arm64`"),
			"filename":              required("Name of the provider archive, for example `terraform-provider-internal_1.2.3_linux_amd64.zip`"),
			"download_url":          required("Where the provider archive can be downloaded from"),
			"shasums_url":           optional("Where the `SHA256SUMS` file for the release can be downloaded from"),
			"shasums_signature_url": optional("Where the signature of the `SHA256SUMS` file can be downloaded from"),
			"shasum":                optional("SHA-256 checksum of the provider archive"),
			"key_id":                optional("ID of the GPG key that signed the `SHA256SUMS` file"),
			"ascii_armor":           optional("The public GPG key, ASCII armored, that signed the `SHA256SUMS` file"),
			"trust_signature":       optional("Trust signature of the key"),
			"source":                optional("Who the signing key belongs to"),
			"source_url":            optional("Where to read about the signing key"),
		},
	}
}

func (r *RegistryProviderImplementationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Registry Provider Implementation Resource Configure Type",
			fmt.Sprintf("Expected *TerrakubeConnectionData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}
	r.client = newTerrakubeHTTPClient(providerData)
	r.endpoint = providerData.Endpoint
	r.token = providerData.Token
}

func (r *RegistryProviderImplementationResource) implementationURL(m *RegistryProviderImplementationResourceModel) string {
	url := fmt.Sprintf("%s/api/v1/organization/%s/provider/%s/version/%s/implementation",
		r.endpoint, m.OrganizationId.ValueString(), m.ProviderId.ValueString(), m.VersionId.ValueString())
	if !m.ID.IsNull() && !m.ID.IsUnknown() && m.ID.ValueString() != "" {
		url += "/" + m.ID.ValueString()
	}
	return url
}

// optionalString sends an unset attribute as null, which clears it on an update.
func optionalString(value types.String) *string {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	return value.ValueStringPointer()
}

// notNullString sends an unset attribute as an empty string. These columns are NOT NULL in
// Terrakube's database, so a null fails the insert. stringFromAPI maps the echoed "" back to
// null when the configuration leaves the attribute out.
func notNullString(value types.String) *string {
	if value.IsNull() || value.IsUnknown() {
		empty := ""
		return &empty
	}
	return value.ValueStringPointer()
}

func (m *RegistryProviderImplementationResourceModel) entity(id string) *client.ProviderImplementationEntity {
	return &client.ProviderImplementationEntity{
		ID:                  id,
		Os:                  m.Os.ValueString(),
		Arch:                m.Arch.ValueString(),
		Filename:            m.Filename.ValueString(),
		DownloadUrl:         notNullString(m.DownloadUrl),
		ShasumsUrl:          notNullString(m.ShasumsUrl),
		ShasumsSignatureUrl: notNullString(m.ShasumsSignatureUrl),
		Shasum:              notNullString(m.Shasum),
		KeyId:               notNullString(m.KeyId),
		AsciiArmor:          optionalString(m.AsciiArmor),
		TrustSignature:      notNullString(m.TrustSignature),
		Source:              notNullString(m.Source),
		SourceUrl:           notNullString(m.SourceUrl),
	}
}

func (m *RegistryProviderImplementationResourceModel) setFromAPI(impl *client.ProviderImplementationEntity) {
	m.ID = types.StringValue(impl.ID)
	m.Os = types.StringValue(impl.Os)
	m.Arch = types.StringValue(impl.Arch)
	m.Filename = types.StringValue(impl.Filename)
	m.DownloadUrl = stringFromAPI(m.DownloadUrl, impl.DownloadUrl)
	m.ShasumsUrl = stringFromAPI(m.ShasumsUrl, impl.ShasumsUrl)
	m.ShasumsSignatureUrl = stringFromAPI(m.ShasumsSignatureUrl, impl.ShasumsSignatureUrl)
	m.Shasum = stringFromAPI(m.Shasum, impl.Shasum)
	m.KeyId = stringFromAPI(m.KeyId, impl.KeyId)
	m.AsciiArmor = stringFromAPI(m.AsciiArmor, impl.AsciiArmor)
	m.TrustSignature = stringFromAPI(m.TrustSignature, impl.TrustSignature)
	m.Source = stringFromAPI(m.Source, impl.Source)
	m.SourceUrl = stringFromAPI(m.SourceUrl, impl.SourceUrl)
}

func (r *RegistryProviderImplementationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan RegistryProviderImplementationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, err := marshalJSONAPI(plan.entity(""))
	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}
	response, responseBody, err := doJSONAPIRequest(ctx, r.client, r.token, http.MethodPost, r.implementationURL(&plan), body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating registry provider implementation", err.Error())
		return
	}
	if reportAPIErrorStatus(&resp.Diagnostics, "creating registry provider implementation", response, responseBody) {
		return
	}

	created := &client.ProviderImplementationEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(string(responseBody)), created); err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response",
			fmt.Sprintf("Error unmarshal payload response, response status: %s, response body: %s, error: %s", response.Status, responseBody, err))
		return
	}

	plan.setFromAPI(created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *RegistryProviderImplementationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state RegistryProviderImplementationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	response, responseBody, err := doJSONAPIRequest(ctx, r.client, r.token, http.MethodGet, r.implementationURL(&state), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error reading registry provider implementation", err.Error())
		return
	}
	if response.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Registry provider implementation not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if reportAPIErrorStatus(&resp.Diagnostics, "reading registry provider implementation", response, responseBody) {
		return
	}

	impl := &client.ProviderImplementationEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(string(responseBody)), impl); err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response",
			fmt.Sprintf("Error unmarshal payload response, response status: %s, response body: %s, error: %s", response.Status, responseBody, err))
		return
	}

	state.setFromAPI(impl)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *RegistryProviderImplementationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state RegistryProviderImplementationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	url := r.implementationURL(&state)
	body, err := marshalJSONAPI(plan.entity(state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}
	response, responseBody, err := doJSONAPIRequest(ctx, r.client, r.token, http.MethodPatch, url, body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating registry provider implementation", err.Error())
		return
	}
	if reportAPIErrorStatus(&resp.Diagnostics, "updating registry provider implementation", response, responseBody) {
		return
	}

	response, responseBody, err = doJSONAPIRequest(ctx, r.client, r.token, http.MethodGet, url, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error reading registry provider implementation after update", err.Error())
		return
	}
	if reportAPIErrorStatus(&resp.Diagnostics, "reading registry provider implementation after update", response, responseBody) {
		return
	}
	updated := &client.ProviderImplementationEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(string(responseBody)), updated); err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.setFromAPI(updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *RegistryProviderImplementationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state RegistryProviderImplementationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, r.implementationURL(&state), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating registry provider implementation request", fmt.Sprintf("Error creating registry provider implementation request: %s", err))
		return
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))

	response, err := r.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Error executing registry provider implementation request", fmt.Sprintf("Error executing registry provider implementation request: %s", err))
		return
	}
	reportDeleteFailure(&resp.Diagnostics, "deleting registry provider implementation", response)
}

func (r *RegistryProviderImplementationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")
	if len(idParts) != 4 || idParts[0] == "" || idParts[1] == "" || idParts[2] == "" || idParts[3] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: 'organization_ID,provider_ID,version_ID,ID', Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("provider_id"), idParts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("version_id"), idParts[2])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[3])...)
}
