package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	terrakube "github.com/terrakube-io/terrakube-go"
)

var (
	_ resource.Resource                = &RegistryProviderImplementationResource{}
	_ resource.ResourceWithConfigure   = &RegistryProviderImplementationResource{}
	_ resource.ResourceWithImportState = &RegistryProviderImplementationResource{}
)

type RegistryProviderImplementationResource struct {
	client *terrakube.Client
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
				Description:   "Terrakube organization ID. Changing this forces a new resource.",
				PlanModifiers: replaceOnChange,
			},
			"provider_id": schema.StringAttribute{
				Required:      true,
				Description:   "ID of the `terrakube_registry_provider`. Changing this forces a new resource.",
				PlanModifiers: replaceOnChange,
			},
			"version_id": schema.StringAttribute{
				Required:      true,
				Description:   "ID of the `terrakube_registry_provider_version` this is a download of. Changing this forces a new resource.",
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
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Registry Provider Implementation Resource")
}

// notNullString sends an unset attribute as an empty string. These columns are NOT NULL in
// Terrakube's database, so a null fails the insert. stringFromAPI maps the echoed "" back to
// null when the configuration leaves the attribute out.
func notNullString(value types.String) string {
	if value.IsNull() || value.IsUnknown() {
		return ""
	}
	return value.ValueString()
}

// attributes builds the attributes of a create or an update. Every attribute is sent, as a full
// write: one removed from the configuration is sent empty, which clears it, since a PATCH that
// left it out would keep the old value. ascii_armor is nullable: a create leaves an unset one out,
// and an update sends it as null.
func (m *RegistryProviderImplementationResourceModel) attributes(create bool) patchAttributes {
	attributes := patchAttributes{
		"os":                  m.Os.ValueString(),
		"arch":                m.Arch.ValueString(),
		"filename":            m.Filename.ValueString(),
		"downloadUrl":         notNullString(m.DownloadUrl),
		"shasumsUrl":          notNullString(m.ShasumsUrl),
		"shasumsSignatureUrl": notNullString(m.ShasumsSignatureUrl),
		"shasum":              notNullString(m.Shasum),
		"keyId":               notNullString(m.KeyId),
		"trustSignature":      notNullString(m.TrustSignature),
		"source":              notNullString(m.Source),
		"sourceUrl":           notNullString(m.SourceUrl),
	}
	switch {
	case !m.AsciiArmor.IsNull() && !m.AsciiArmor.IsUnknown():
		attributes["asciiArmor"] = m.AsciiArmor.ValueString()
	case !create:
		attributes["asciiArmor"] = nil
	}
	return attributes
}

func (m *RegistryProviderImplementationResourceModel) setFromAPI(impl *terrakube.Implementation) {
	m.ID = types.StringValue(impl.ID)
	m.Os = types.StringValue(impl.Os)
	m.Arch = types.StringValue(impl.Arch)
	m.Filename = types.StringValue(impl.Filename)
	m.DownloadUrl = stringFromAPI(m.DownloadUrl, impl.DownloadURL)
	m.ShasumsUrl = stringFromAPI(m.ShasumsUrl, impl.ShasumsURL)
	m.ShasumsSignatureUrl = stringFromAPI(m.ShasumsSignatureUrl, impl.ShasumsSignatureURL)
	m.Shasum = stringFromAPI(m.Shasum, impl.Shasum)
	m.KeyId = stringFromAPI(m.KeyId, impl.KeyID)
	m.AsciiArmor = stringFromAPI(m.AsciiArmor, impl.ASCIIArmor)
	m.TrustSignature = stringFromAPI(m.TrustSignature, impl.TrustSignature)
	m.Source = stringFromAPI(m.Source, impl.Source)
	m.SourceUrl = stringFromAPI(m.SourceUrl, impl.SourceURL)
}

func (r *RegistryProviderImplementationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan RegistryProviderImplementationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.Implementations.CreateFields(ctx, plan.OrganizationId.ValueString(), plan.ProviderId.ValueString(),
		plan.VersionId.ValueString(), terrakube.Patch{Attributes: plan.attributes(true)})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating registry provider implementation", err)
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

	impl, err := r.client.Implementations.Get(ctx, state.OrganizationId.ValueString(), state.ProviderId.ValueString(),
		state.VersionId.ValueString(), state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Registry provider implementation not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading registry provider implementation", err)
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

	organizationID, providerID, versionID, id := state.OrganizationId.ValueString(), state.ProviderId.ValueString(),
		state.VersionId.ValueString(), state.ID.ValueString()
	_, err := r.client.Implementations.UpdateFields(ctx, organizationID, providerID, versionID, id,
		terrakube.Patch{Attributes: plan.attributes(false)})
	if patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating registry provider implementation", err)
		return
	}

	updated, err := r.client.Implementations.Get(ctx, organizationID, providerID, versionID, id)
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading registry provider implementation after update", err)
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

	err := r.client.Implementations.Delete(ctx, state.OrganizationId.ValueString(), state.ProviderId.ValueString(),
		state.VersionId.ValueString(), state.ID.ValueString())
	reportDeleteError(&resp.Diagnostics, "deleting registry provider implementation", err)
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
