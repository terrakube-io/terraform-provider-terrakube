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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ resource.Resource = &RegistryModuleResource{}
var _ resource.ResourceWithImportState = &RegistryModuleResource{}

type RegistryModuleResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type RegistryModuleResourceModel struct {
	ID                types.String `tfsdk:"id"`
	OrganizationId    types.String `tfsdk:"organization_id"`
	Name              types.String `tfsdk:"name"`
	Description       types.String `tfsdk:"description"`
	ProviderName      types.String `tfsdk:"provider_name"`
	Source            types.String `tfsdk:"source"`
	PublicRegistryRef types.String `tfsdk:"public_registry_ref"`
	VcsId             types.String `tfsdk:"vcs_id"`
	SshId             types.String `tfsdk:"ssh_id"`
	TagPrefix         types.String `tfsdk:"tag_prefix"`
	Folder            types.String `tfsdk:"folder"`
	RegistryPath      types.String `tfsdk:"registry_path"`
	LatestVersion     types.String `tfsdk:"latest_version"`
}

func NewRegistryModuleResource() resource.Resource {
	return &RegistryModuleResource{}
}

func (r *RegistryModuleResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_registry_module"
}

func (r *RegistryModuleResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Publishes a module in the organization's private Terrakube registry.\n\n" +
			"The registry identity of a module is `{organization}/{name}/{provider_name}`.\n\n" +
			"Terrakube only accepts **Git URLs** as `source`. To register a module that you found on the public " +
			"Terraform Registry (e.g. `cloudposse/label/null`), either:\n" +
			"  - set `source` to the corresponding Git URL (e.g. `https://github.com/cloudposse/terraform-null-label.git`), or\n" +
			"  - set `public_registry_ref` to the `{namespace}/{name}/{provider}` triple and let the provider " +
			"compute the GitHub URL using the conventional `github.com/{namespace}/terraform-{provider}-{name}` mapping.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Module ID",
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
				Description: "Module name",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Module description. When importing from `public_registry_ref` and no description is set, Terrakube stores a default one.",
			},
			"provider_name": schema.StringAttribute{
				Required:    true,
				Description: "Module provider name (e.g. `aws`, `azurerm`, `null`)",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"source": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Git URL of the module sources. Mutually exclusive with `public_registry_ref`; exactly one must be set. If `public_registry_ref` is set, `source` is computed from the conventional GitHub URL.",
			},
			"public_registry_ref": schema.StringAttribute{
				Optional:    true,
				Description: "Convenience shortcut in the form `{namespace}/{name}/{provider}` (e.g. `cloudposse/label/null`). When set, `source` is computed as `https://github.com/{namespace}/terraform-{provider}-{name}.git`. Mutually exclusive with `source`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"vcs_id": schema.StringAttribute{
				Optional:    true,
				Description: "VCS connection ID for private modules",
			},
			"ssh_id": schema.StringAttribute{
				Optional:    true,
				Description: "SSH key ID for private modules",
			},
			"tag_prefix": schema.StringAttribute{
				Optional:    true,
				Description: "Tag prefix for mono-repository modules (e.g. `module/` picks up tags `module/v1.0.0`)",
			},
			"folder": schema.StringAttribute{
				Optional:    true,
				Description: "Subfolder containing the module sources. Wrap with leading and trailing slashes (e.g. `/path/`)",
			},
			"registry_path": schema.StringAttribute{
				Computed:    true,
				Description: "Module's registry path in the form `{organization}/{name}/{provider}`",
			},
			"latest_version": schema.StringAttribute{
				Computed:    true,
				Description: "Latest version detected by Terrakube's VCS scan",
			},
		},
	}
}

func (r *RegistryModuleResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Registry Module Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Registry Module resource", map[string]any{"success": true})
}

// resolvePublicRegistrySource converts a `{namespace}/{name}/{provider}` reference
// to the conventional public GitHub URL (`github.com/{namespace}/terraform-{provider}-{name}`).
// This matches the behavior of the Terrakube UI when importing modules from the public registry.
func resolvePublicRegistrySource(ref string) (namespace, name, providerName, source string, err error) {
	parts := strings.Split(ref, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", "", fmt.Errorf("public_registry_ref must be in the form `{namespace}/{name}/{provider}`, got %q", ref)
	}
	namespace, name, providerName = parts[0], parts[1], parts[2]
	source = fmt.Sprintf("https://github.com/%s/terraform-%s-%s.git", namespace, providerName, name)
	return
}

func (r *RegistryModuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan RegistryModuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sourceSet := !plan.Source.IsNull() && !plan.Source.IsUnknown() && plan.Source.ValueString() != ""
	refSet := !plan.PublicRegistryRef.IsNull() && !plan.PublicRegistryRef.IsUnknown() && plan.PublicRegistryRef.ValueString() != ""
	if sourceSet == refSet {
		resp.Diagnostics.AddError(
			"Exactly one of `source` or `public_registry_ref` must be set",
			"Set `source` to a Git URL for arbitrary repositories, or `public_registry_ref` (e.g. `cloudposse/label/null`) for modules whose sources follow the public Terraform Registry GitHub-naming convention.",
		)
		return
	}

	if refSet {
		_, refName, refProvider, source, err := resolvePublicRegistrySource(plan.PublicRegistryRef.ValueString())
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("public_registry_ref"), "Invalid public_registry_ref", err.Error())
			return
		}
		plan.Source = types.StringValue(source)
		if plan.Name.ValueString() == "" {
			plan.Name = types.StringValue(refName)
		}
		if plan.ProviderName.ValueString() == "" {
			plan.ProviderName = types.StringValue(refProvider)
		}
		if plan.Description.IsNull() || plan.Description.IsUnknown() {
			plan.Description = types.StringValue(fmt.Sprintf("Imported from Terraform Registry: %s", plan.PublicRegistryRef.ValueString()))
		}
	}

	bodyRequest := &client.ModuleEntity{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
		Provider:    plan.ProviderName.ValueString(),
		Source:      plan.Source.ValueString(),
	}
	if !plan.Folder.IsNull() {
		bodyRequest.Folder = plan.Folder.ValueStringPointer()
	}
	if !plan.TagPrefix.IsNull() {
		bodyRequest.TagPrefix = plan.TagPrefix.ValueStringPointer()
	}
	if !plan.VcsId.IsNull() {
		bodyRequest.Vcs = &client.VcsEntity{ID: plan.VcsId.ValueString()}
	}
	if !plan.SshId.IsNull() {
		bodyRequest.Ssh = &client.SshEntity{ID: plan.SshId.ValueString()}
	}

	var out = new(bytes.Buffer)
	if err := jsonapi.MarshalPayload(out, bodyRequest); err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	tflog.Info(ctx, fmt.Sprintf("Registry Module Create Body: %s", out.String()))

	moduleRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/organization/%s/module", r.endpoint, plan.OrganizationId.ValueString()), strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating registry module resource request", fmt.Sprintf("Error creating registry module resource request: %s", err))
		return
	}
	moduleRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	moduleRequest.Header.Add("Content-Type", "application/vnd.api+json")

	moduleResponse, err := r.client.Do(moduleRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing registry module resource request", fmt.Sprintf("Error executing registry module resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(moduleResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading registry module resource response")
	}
	if moduleResponse.StatusCode >= 400 {
		resp.Diagnostics.AddError("Registry module creation failed", fmt.Sprintf("status: %s, body: %s", moduleResponse.Status, string(bodyResponse)))
		return
	}

	newModule := &client.ModuleEntity{}
	if err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), newModule); err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ID = types.StringValue(newModule.ID)
	plan.Name = types.StringValue(newModule.Name)
	plan.Description = types.StringValue(newModule.Description)
	plan.ProviderName = types.StringValue(newModule.Provider)
	plan.Source = types.StringValue(newModule.Source)
	if newModule.Folder != nil {
		plan.Folder = types.StringPointerValue(newModule.Folder)
	}
	if newModule.TagPrefix != nil {
		plan.TagPrefix = types.StringPointerValue(newModule.TagPrefix)
	}
	if newModule.RegistryPath != "" {
		plan.RegistryPath = types.StringValue(newModule.RegistryPath)
	} else {
		plan.RegistryPath = types.StringNull()
	}
	if newModule.LatestVersion != "" {
		plan.LatestVersion = types.StringValue(newModule.LatestVersion)
	} else {
		plan.LatestVersion = types.StringNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *RegistryModuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state RegistryModuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	moduleRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/module/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating registry module resource request", fmt.Sprintf("Error creating registry module resource request: %s", err))
		return
	}
	moduleRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	moduleRequest.Header.Add("Content-Type", "application/vnd.api+json")

	moduleResponse, err := r.client.Do(moduleRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing registry module resource request", fmt.Sprintf("Error executing registry module resource request: %s", err))
		return
	}
	if moduleResponse.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Registry module not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	bodyResponse, err := io.ReadAll(moduleResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading registry module resource response")
	}

	module := &client.ModuleEntity{}
	if err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), module); err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	state.Name = types.StringValue(module.Name)
	state.Description = types.StringValue(module.Description)
	state.ProviderName = types.StringValue(module.Provider)
	state.Source = types.StringValue(module.Source)
	state.Folder = types.StringPointerValue(module.Folder)
	state.TagPrefix = types.StringPointerValue(module.TagPrefix)
	if module.Vcs != nil {
		state.VcsId = types.StringValue(module.Vcs.ID)
	}
	if module.Ssh != nil {
		state.SshId = types.StringValue(module.Ssh.ID)
	}
	if module.RegistryPath != "" {
		state.RegistryPath = types.StringValue(module.RegistryPath)
	} else {
		state.RegistryPath = types.StringNull()
	}
	if module.LatestVersion != "" {
		state.LatestVersion = types.StringValue(module.LatestVersion)
	} else {
		state.LatestVersion = types.StringNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *RegistryModuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan RegistryModuleResourceModel
	var state RegistryModuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	source := plan.Source.ValueString()
	if !plan.PublicRegistryRef.IsNull() && plan.PublicRegistryRef.ValueString() != "" {
		if _, _, _, computed, err := resolvePublicRegistrySource(plan.PublicRegistryRef.ValueString()); err == nil {
			source = computed
			plan.Source = types.StringValue(computed)
		}
	}

	bodyRequest := &client.ModuleEntity{
		ID:          state.ID.ValueString(),
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
		Provider:    plan.ProviderName.ValueString(),
		Source:      source,
	}
	if !plan.Folder.IsNull() {
		bodyRequest.Folder = plan.Folder.ValueStringPointer()
	}
	if !plan.TagPrefix.IsNull() {
		bodyRequest.TagPrefix = plan.TagPrefix.ValueStringPointer()
	}
	if !plan.VcsId.IsNull() {
		bodyRequest.Vcs = &client.VcsEntity{ID: plan.VcsId.ValueString()}
	}
	if !plan.SshId.IsNull() {
		bodyRequest.Ssh = &client.SshEntity{ID: plan.SshId.ValueString()}
	}

	var out = new(bytes.Buffer)
	if err := jsonapi.MarshalPayload(out, bodyRequest); err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	moduleRequest, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/module/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating registry module resource request", fmt.Sprintf("Error creating registry module resource request: %s", err))
		return
	}
	moduleRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	moduleRequest.Header.Add("Content-Type", "application/vnd.api+json")

	patchResponse, err := r.client.Do(moduleRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing registry module resource request", fmt.Sprintf("Error executing registry module resource request: %s", err))
		return
	}
	patchBody, _ := io.ReadAll(patchResponse.Body)
	if patchResponse.StatusCode >= 400 {
		resp.Diagnostics.AddError("Registry module update failed", fmt.Sprintf("status: %s, body: %s", patchResponse.Status, string(patchBody)))
		return
	}

	getRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/module/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating registry module resource request", fmt.Sprintf("Error creating registry module resource request: %s", err))
		return
	}
	getRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	getRequest.Header.Add("Content-Type", "application/vnd.api+json")

	getResponse, err := r.client.Do(getRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing registry module resource request", fmt.Sprintf("Error executing registry module resource request: %s", err))
		return
	}
	getBody, err := io.ReadAll(getResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading registry module resource response body", fmt.Sprintf("Error reading registry module resource response body: %s", err))
		return
	}

	updated := &client.ModuleEntity{}
	if err = jsonapi.UnmarshalPayload(strings.NewReader(string(getBody)), updated); err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.Name = types.StringValue(updated.Name)
	plan.Description = types.StringValue(updated.Description)
	plan.ProviderName = types.StringValue(updated.Provider)
	plan.Source = types.StringValue(updated.Source)
	plan.Folder = types.StringPointerValue(updated.Folder)
	plan.TagPrefix = types.StringPointerValue(updated.TagPrefix)
	if updated.RegistryPath != "" {
		plan.RegistryPath = types.StringValue(updated.RegistryPath)
	} else {
		plan.RegistryPath = types.StringNull()
	}
	if updated.LatestVersion != "" {
		plan.LatestVersion = types.StringValue(updated.LatestVersion)
	} else {
		plan.LatestVersion = types.StringNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *RegistryModuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data RegistryModuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	delRequest, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/organization/%s/module/%s", r.endpoint, data.OrganizationId.ValueString(), data.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating registry module resource request", fmt.Sprintf("Error creating registry module resource request: %s", err))
		return
	}
	delRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))

	if _, err = r.client.Do(delRequest); err != nil {
		resp.Diagnostics.AddError("Error executing registry module resource request", fmt.Sprintf("Error executing registry module resource request: %s", err))
		return
	}
}

func (r *RegistryModuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
