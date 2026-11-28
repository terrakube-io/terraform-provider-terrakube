package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
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
	_ resource.Resource                     = &RegistryProviderResource{}
	_ resource.ResourceWithConfigure        = &RegistryProviderResource{}
	_ resource.ResourceWithImportState      = &RegistryProviderResource{}
	_ resource.ResourceWithConfigValidators = &RegistryProviderResource{}
)

type RegistryProviderResource struct {
	client *terrakube.Client
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
		MarkdownDescription: "A provider in the private registry of an organization. Set `imported` and `registry_namespace` to mirror a provider from the public registry, and Terrakube keeps its versions up to date. Destroying the provider also deletes its versions and their implementations, since Terrakube does not delete them for you.",
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
				Description: "Terrakube organization ID. Changing this forces a new resource.",
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
					importedFollowsConfig{},
				},
			},
			"registry_namespace": schema.StringAttribute{
				Optional:    true,
				Description: "Namespace of the provider in the public registry, for example `hashicorp`. Only used when `imported` is true. Terrakube 2.30.0+.",
			},
		},
	}
}

// importedFollowsConfig keeps the prior value of imported when it is left out of the configuration,
// like UseStateForUnknown, except when the namespace is removed too. That means the provider is no
// longer mirrored, so it plans false. Otherwise the provider would stay imported on the server with
// an empty plan, and registry_namespace null, a combination the validator rejects in configuration.
type importedFollowsConfig struct{}

func (importedFollowsConfig) Description(context.Context) string {
	return "Keeps the prior value, and plans false when imported and registry_namespace are both removed."
}

func (m importedFollowsConfig) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (importedFollowsConfig) PlanModifyBool(ctx context.Context, req planmodifier.BoolRequest, resp *planmodifier.BoolResponse) {
	if req.State.Raw.IsNull() || !req.ConfigValue.IsNull() || req.StateValue.IsNull() {
		return
	}
	var namespace types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("registry_namespace"), &namespace)...)
	if resp.Diagnostics.HasError() || namespace.IsUnknown() {
		return
	}
	if namespace.IsNull() && req.StateValue.ValueBool() {
		resp.PlanValue = types.BoolValue(false)
		return
	}
	resp.PlanValue = req.StateValue
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
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Registry Provider Resource")
}

// registryProviderAttributes builds the attributes of a create (prior is nil) or an update.
// Imported and the namespace are only sent when they are used, because a Terrakube older than
// 2.30.0 does not know them.
func registryProviderAttributes(plan, prior *RegistryProviderResourceModel) patchAttributes {
	attributes := patchAttributes{}
	if prior == nil {
		attributes.createString("name", plan.Name)
		attributes.createString("description", plan.Description)
	} else {
		attributes.updateString("name", plan.Name, prior.Name)
		attributes.updateString("description", plan.Description, prior.Description)
	}
	// imported is only sent when it is true, or when it is switched off, so a provider that was
	// never imported works on a Terrakube older than 2.30.0.
	if !plan.Imported.IsNull() && !plan.Imported.IsUnknown() &&
		(plan.Imported.ValueBool() || (prior != nil && prior.Imported.ValueBool())) {
		attributes["imported"] = plan.Imported.ValueBool()
	}
	switch {
	case !plan.RegistryNamespace.IsNull() && !plan.RegistryNamespace.IsUnknown():
		attributes["registryNamespace"] = plan.RegistryNamespace.ValueString()
	case prior != nil && !prior.RegistryNamespace.IsNull():
		// Leaving the attribute out of a PATCH keeps the old value, so a removed namespace is sent empty.
		attributes["registryNamespace"] = ""
	}
	return attributes
}

// stringFromAPI keeps a null a null: Terrakube may return an empty string for an attribute that
// was never set, and Terraform would report a difference between the plan and the result.
func stringFromAPI(current types.String, value *string) types.String {
	if value == nil || (*value == "" && current.IsNull()) {
		return types.StringNull()
	}
	return types.StringValue(*value)
}

func (m *RegistryProviderResourceModel) setFromAPI(provider *terrakube.Provider) {
	m.ID = types.StringValue(provider.ID)
	m.Name = types.StringValue(provider.Name)
	m.Description = stringFromAPI(m.Description, provider.Description)
	m.Imported = types.BoolValue(provider.Imported)
	m.RegistryNamespace = stringFromAPI(m.RegistryNamespace, provider.RegistryNamespace)
}

func (r *RegistryProviderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan RegistryProviderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	patch := terrakube.Patch{Attributes: registryProviderAttributes(&plan, nil)}
	created, err := r.client.Providers.CreateFields(ctx, plan.OrganizationId.ValueString(), patch)
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating registry provider", err)
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

	provider, err := r.client.Providers.Get(ctx, state.OrganizationId.ValueString(), state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Registry provider not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading registry provider", err)
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

	organizationID, id := state.OrganizationId.ValueString(), state.ID.ValueString()
	patch := terrakube.Patch{Attributes: registryProviderAttributes(&plan, &state)}
	if _, err := r.client.Providers.UpdateFields(ctx, organizationID, id, patch); patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating registry provider", err)
		return
	}

	updated, err := r.client.Providers.Get(ctx, organizationID, id)
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading registry provider after update", err)
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

	// Terrakube does not cascade: a version references its provider, and an implementation its
	// version. A mirrored provider gets its versions from Terrakube itself, so they have to go first.
	r.deleteVersions(ctx, &resp.Diagnostics, state.OrganizationId.ValueString(), state.ID.ValueString())
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Providers.Delete(ctx, state.OrganizationId.ValueString(), state.ID.ValueString())
	if err != nil && !terrakube.IsNotFound(err) {
		reportClientError(&resp.Diagnostics, "deleting registry provider", err)
	}
}

// maxVersionDeletePages bounds the list-and-delete loop, so a version that cannot be deleted
// cannot keep it going.
const maxVersionDeletePages = 20

// deleteVersions deletes the implementations and then the versions of a provider. A list that
// answers 404 has nothing left to delete.
func (r *RegistryProviderResource) deleteVersions(ctx context.Context, diags *diag.Diagnostics, organizationID, providerID string) {
	for range maxVersionDeletePages {
		versions, err := r.client.ProviderVersions.List(ctx, organizationID, providerID, nil)
		if err != nil && !terrakube.IsNotFound(err) {
			reportClientError(diags, "listing the versions of the registry provider", err)
			return
		}
		if len(versions) == 0 {
			return
		}
		for _, version := range versions {
			implementations, err := r.client.Implementations.List(ctx, organizationID, providerID, version.ID, nil)
			if err != nil && !terrakube.IsNotFound(err) {
				reportClientError(diags, "listing the implementations of a registry provider version", err)
				return
			}
			for _, implementation := range implementations {
				err := r.client.Implementations.Delete(ctx, organizationID, providerID, version.ID, implementation.ID)
				if !deleteSucceeded(diags, "deleting registry provider implementation", err) {
					return
				}
			}
			err = r.client.ProviderVersions.Delete(ctx, organizationID, providerID, version.ID)
			if !deleteSucceeded(diags, "deleting registry provider version", err) {
				return
			}
		}
	}
	diags.AddError("Error deleting the versions of the registry provider",
		fmt.Sprintf("Versions are still left after %d rounds of deleting them.", maxVersionDeletePages))
}

// deleteSucceeded reports whether a delete left the object gone. A 404 means it already was;
// any other error, such as a 409 for a version that still has implementations, is reported.
func deleteSucceeded(diags *diag.Diagnostics, action string, err error) bool {
	if err != nil && !terrakube.IsNotFound(err) {
		reportClientError(diags, action, err)
		return false
	}
	return true
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
