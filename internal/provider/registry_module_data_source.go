package provider

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"terraform-provider-terrakube/internal/client"

	"github.com/google/jsonapi"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ datasource.DataSource              = &RegistryModuleDataSource{}
	_ datasource.DataSourceWithConfigure = &RegistryModuleDataSource{}
)

type RegistryModuleDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Name           types.String `tfsdk:"name"`
	ProviderName   types.String `tfsdk:"provider_name"`
	Description    types.String `tfsdk:"description"`
	Source         types.String `tfsdk:"source"`
	Folder         types.String `tfsdk:"folder"`
	TagPrefix      types.String `tfsdk:"tag_prefix"`
	VcsId          types.String `tfsdk:"vcs_id"`
	SshId          types.String `tfsdk:"ssh_id"`
	RegistryPath   types.String `tfsdk:"registry_path"`
	LatestVersion  types.String `tfsdk:"latest_version"`
}

type RegistryModuleDataSource struct {
	client   *http.Client
	endpoint string
	token    string
}

func NewRegistryModuleDataSource() datasource.DataSource {
	return &RegistryModuleDataSource{}
}

func (d *RegistryModuleDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, res *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		res.Diagnostics.AddError(
			"Unexpected Registry Module Data Source Configure Type",
			fmt.Sprintf("Expected *TerrakubeConnectionData got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	if providerData.InsecureHttpClient {
		if custom, ok := http.DefaultTransport.(*http.Transport); ok {
			customTransport := custom.Clone()
			customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
			d.client = &http.Client{Transport: customTransport}
		} else {
			d.client = &http.Client{}
		}
	} else {
		d.client = &http.Client{}
	}
	d.endpoint = providerData.Endpoint
	d.token = providerData.Token

	tflog.Info(ctx, "Creating Registry Module datasource")
}

func (d *RegistryModuleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_registry_module"
}

func (d *RegistryModuleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a module published in the organization's Terrakube registry by name and provider.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Module ID",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization ID",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Module name",
			},
			"provider_name": schema.StringAttribute{
				Required:    true,
				Description: "Module provider name (e.g. `aws`, `azurerm`, `null`). The triple `{organization}/{name}/{provider_name}` is what uniquely identifies a module in the registry.",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Module description",
			},
			"source": schema.StringAttribute{
				Computed:    true,
				Description: "Source Git URL of the module",
			},
			"folder": schema.StringAttribute{
				Computed:    true,
				Description: "Subfolder containing the module sources, if any",
			},
			"tag_prefix": schema.StringAttribute{
				Computed:    true,
				Description: "Tag prefix for mono-repository modules",
			},
			"vcs_id": schema.StringAttribute{
				Computed:    true,
				Description: "VCS connection ID, if the module is private",
			},
			"ssh_id": schema.StringAttribute{
				Computed:    true,
				Description: "SSH key ID, if the module is private",
			},
			"registry_path": schema.StringAttribute{
				Computed:    true,
				Description: "Registry path in the form `{organization}/{name}/{provider}`",
			},
			"latest_version": schema.StringAttribute{
				Computed:    true,
				Description: "Latest version detected by Terrakube's VCS scan",
			},
		},
	}
}

func (d *RegistryModuleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state RegistryModuleDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	filter := url.QueryEscape(fmt.Sprintf("name==%s;provider==%s", state.Name.ValueString(), state.ProviderName.ValueString()))
	endpoint := fmt.Sprintf("%s/api/v1/organization/%s/module?filter[module]=%s", d.endpoint, state.OrganizationId.ValueString(), filter)

	moduleRequest, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating registry module data source request", fmt.Sprintf("Error creating registry module data source request: %s", err))
		return
	}
	moduleRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	moduleRequest.Header.Add("Content-Type", "application/vnd.api+json")

	moduleResponse, err := d.client.Do(moduleRequest)
	if err != nil {
		resp.Diagnostics.AddError("Request errored", fmt.Sprintf("error: %v", err))
		return
	}

	body, err := io.ReadAll(moduleResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading body", fmt.Sprintf("status: %v, error: %v", moduleResponse.Status, err))
		return
	}
	if moduleResponse.StatusCode >= 400 {
		resp.Diagnostics.AddError("Request failed", fmt.Sprintf("status: %v, body: %v", moduleResponse.Status, string(body)))
		return
	}

	modules, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(new(client.ModuleEntity)))
	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal payload", fmt.Sprintf("status: %s, body: %s, error: %s", moduleResponse.Status, string(body), err))
		return
	}

	if len(modules) == 0 {
		resp.Diagnostics.AddError(
			"Module not found",
			fmt.Sprintf("No module named %q with provider %q was found in organization %s", state.Name.ValueString(), state.ProviderName.ValueString(), state.OrganizationId.ValueString()),
		)
		return
	}

	module, _ := modules[0].(*client.ModuleEntity)
	state.ID = types.StringValue(module.ID)
	state.Name = types.StringValue(module.Name)
	state.ProviderName = types.StringValue(module.Provider)
	state.Description = types.StringValue(module.Description)
	state.Source = types.StringValue(module.Source)
	state.Folder = types.StringPointerValue(module.Folder)
	state.TagPrefix = types.StringPointerValue(module.TagPrefix)
	if module.Vcs != nil {
		state.VcsId = types.StringValue(module.Vcs.ID)
	} else {
		state.VcsId = types.StringNull()
	}
	if module.Ssh != nil {
		state.SshId = types.StringValue(module.Ssh.ID)
	} else {
		state.SshId = types.StringNull()
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
