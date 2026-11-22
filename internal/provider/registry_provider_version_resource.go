package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	terrakube "github.com/terrakube-io/terrakube-go"
)

var (
	_ resource.Resource                = &RegistryProviderVersionResource{}
	_ resource.ResourceWithConfigure   = &RegistryProviderVersionResource{}
	_ resource.ResourceWithImportState = &RegistryProviderVersionResource{}
)

// protocolPattern is a plugin protocol version such as 5.0 or 6.0.
var protocolPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)

type RegistryProviderVersionResource struct {
	client *terrakube.Client
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
				Description:   "Terrakube organization ID. Changing this forces a new resource.",
				PlanModifiers: replaceOnChange,
			},
			"provider_id": schema.StringAttribute{
				Required:      true,
				Description:   "ID of the `terrakube_registry_provider` this is a version of. Changing this forces a new resource.",
				PlanModifiers: replaceOnChange,
			},
			"version_number": schema.StringAttribute{
				Required:      true,
				Description:   "Version, for example `1.2.3`. Changing this forces a new resource, so a new version is created.",
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
				Description: "`active`, `deprecated` (still served, with a warning) or `removed` (no longer served). Removing it from the configuration sets it back to `active`. Terrakube 2.34.0+.",
				Validators:  []validator.String{stringvalidator.OneOf("active", "deprecated", "removed")},
				PlanModifiers: []planmodifier.String{
					statusDefaultsToActive{},
				},
			},
			"deprecation_message": schema.StringAttribute{
				Optional:    true,
				Description: "Shown with a deprecated or removed version, for example when it will go or what to upgrade to. One line of up to 1024 UTF-16 code units (an emoji counts as two). Terrakube 2.34.0+.",
				Validators:  []validator.String{deprecationMessageValidator{}},
			},
		},
	}
}

// statusDefaultsToActive plans "active" when status is not in the configuration, so removing
// `status = "deprecated"` undoes the deprecation instead of keeping the old value. On create it
// leaves the value unknown, so a server that has no status is not sent one.
type statusDefaultsToActive struct{}

func (statusDefaultsToActive) Description(context.Context) string {
	return "Plans active when status is removed from the configuration."
}

func (m statusDefaultsToActive) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (statusDefaultsToActive) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.State.Raw.IsNull() || !req.ConfigValue.IsNull() {
		return
	}
	resp.PlanValue = types.StringValue("active")
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
	case len(utf16.Encode([]rune(value))) > 1024:
		// The API limit is Java's String.length(), which counts UTF-16 code units, so a character
		// outside the Basic Multilingual Plane (an emoji, for example) counts as two.
		problem = "must be at most 1024 characters (UTF-16 code units, so an emoji counts as two)"
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
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Registry Provider Version Resource")
}

func (m *RegistryProviderVersionResourceModel) protocolList(ctx context.Context) ([]string, error) {
	var protocols []string
	if diags := m.Protocols.ElementsAs(ctx, &protocols, false); diags.HasError() {
		return nil, fmt.Errorf("reading protocols: %v", diags)
	}
	return protocols, nil
}

// registryProviderVersionAttributes builds the attributes of a create (prior is nil) or an update.
// Status and the deprecation message are only sent when they are set (and, for status, changed),
// because a Terrakube older than 2.34.0 does not know them. A message removed from the
// configuration is sent empty, since a PATCH that leaves it out keeps the old one.
func registryProviderVersionAttributes(ctx context.Context, plan, prior *RegistryProviderVersionResourceModel) (patchAttributes, error) {
	protocols, err := plan.protocolList(ctx)
	if err != nil {
		return nil, err
	}
	attributes := patchAttributes{
		"versionNumber": plan.VersionNumber.ValueString(),
		"protocols":     strings.Join(protocols, ","),
	}
	// An update only sends status when it changes. A version that never set one reads back as
	// active, and a Terrakube older than 2.34.0 rejects the attribute.
	if !plan.Status.IsNull() && !plan.Status.IsUnknown() && (prior == nil || prior.Status.ValueString() != plan.Status.ValueString()) {
		attributes["status"] = plan.Status.ValueString()
	}
	switch {
	case !plan.DeprecationMessage.IsNull() && !plan.DeprecationMessage.IsUnknown():
		attributes["deprecationMessage"] = plan.DeprecationMessage.ValueString()
	case prior != nil && !prior.DeprecationMessage.IsNull():
		attributes["deprecationMessage"] = ""
	}
	return attributes, nil
}

func (m *RegistryProviderVersionResourceModel) setFromAPI(version *terrakube.ProviderVersion) {
	m.ID = types.StringValue(version.ID)
	m.VersionNumber = types.StringValue(version.VersionNumber)

	elements := []types.String{}
	if version.Protocols != nil {
		for _, protocol := range strings.Split(*version.Protocols, ",") {
			if protocol = strings.TrimSpace(protocol); protocol != "" {
				elements = append(elements, types.StringValue(protocol))
			}
		}
	}
	values := make([]attr.Value, len(elements))
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

	attributes, err := registryProviderVersionAttributes(ctx, &plan, nil)
	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}
	created, err := r.client.ProviderVersions.CreateFields(ctx, plan.OrganizationId.ValueString(), plan.ProviderId.ValueString(),
		terrakube.Patch{Attributes: attributes})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating registry provider version", err)
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

	version, err := r.client.ProviderVersions.Get(ctx, state.OrganizationId.ValueString(), state.ProviderId.ValueString(), state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Registry provider version not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading registry provider version", err)
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

	attributes, err := registryProviderVersionAttributes(ctx, &plan, &state)
	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}
	organizationID, providerID, id := state.OrganizationId.ValueString(), state.ProviderId.ValueString(), state.ID.ValueString()
	_, err = r.client.ProviderVersions.UpdateFields(ctx, organizationID, providerID, id, terrakube.Patch{Attributes: attributes})
	if patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating registry provider version", err)
		return
	}

	updated, err := r.client.ProviderVersions.Get(ctx, organizationID, providerID, id)
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading registry provider version after update", err)
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

	err := r.client.ProviderVersions.Delete(ctx, state.OrganizationId.ValueString(), state.ProviderId.ValueString(), state.ID.ValueString())
	reportDeleteError(&resp.Diagnostics, "deleting registry provider version", err)
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
