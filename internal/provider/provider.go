// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure TerrakubeProvider satisfies various provider interfaces.
var _ provider.Provider = &TerrakubeProvider{}

// TerrakubeProvider defines the provider implementation.
type TerrakubeProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// hashicupsProviderModel maps provider schema data to a Go type.
type TerrakubeProviderModel struct {
	Endpoint           types.String `tfsdk:"endpoint"`
	Token              types.String `tfsdk:"token"`
	InsecureHttpClient types.Bool   `tfsdk:"insecure_http_client"`
}

type TerrakubeConnectionData struct {
	Endpoint           string
	Token              string
	InsecureHttpClient bool
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &TerrakubeProvider{
			version: version,
		}
	}
}

func (p *TerrakubeProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "terrakube"
	resp.Version = p.version
}

func (p *TerrakubeProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional:    true,
				Description: "Terrakube API Endpoint. Example: https://terrakube-api.minikube.net, can also be specified with environment variable `TERRAKUBE_ENDPOINT`.",
			},
			"token": schema.StringAttribute{
				Optional:    true,
				Description: "Access Token generated in Terrakube UI (https://docs.terrakube.io/user-guide/organizations/api-tokens), can also be specificed with environment variable `TERRAKUBE_TOKEN`.",
			},
			"insecure_http_client": schema.BoolAttribute{
				Optional:    true,
				Description: "Disable https certificate validation, default is `false`.",
			},
		},
	}
}

func (p *TerrakubeProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	tflog.Info(ctx, "Retrieving provider data from configuration")

	var config TerrakubeProviderModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if config.Endpoint.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("endpoint"),
			"Unknown Terrakube API Host",
			"The provider cannot create the Terrakube API client as there is an unknown configuration value for the Terrakube API host. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the TERRAKUBE_HOSTNAME environment variable.",
		)
	}

	if config.Token.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"Unknown Terrakube API token",
			"The provider cannot create the Terrakube API client as there is an unknown configuration value for the Terrakube API username. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the TERRAKUBE_TOKEN environment variable.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// Default values to environment variables, but override
	// with Terraform configuration value if set.

	endpoint := os.Getenv("TERRAKUBE_ENDPOINT")
	token := os.Getenv("TERRAKUBE_TOKEN")
	insecureHttpClient := false

	if !config.Endpoint.IsNull() {
		endpoint = config.Endpoint.ValueString()
	}

	if !config.Token.IsNull() {
		token = config.Token.ValueString()
	}

	if !config.InsecureHttpClient.IsNull() {
		insecureHttpClient = config.InsecureHttpClient.ValueBool()
	}

	// If any of the expected configurations are missing, return
	// errors with provider-specific guidance.

	if endpoint == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("endpoint"),
			"Missing Terrakube API Host",
			"The provider cannot create the Terrakube API client as there is a missing or empty value for the Terrakube API host. "+
				"Set the host value in the configuration or use the TERRAKUBE_ENDPOINT environment variable. "+
				"If either is already set, ensure the value is not empty.",
		)
	}

	if token == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"Missing HashiCups API Username",
			"The provider cannot create the Terrakube API client as there is a missing or empty value for the Terrakube API username. "+
				"Set the username value in the configuration or use the TERRAKUBE_ENDPOINT environment variable. "+
				"If either is already set, ensure the value is not empty.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	connection := new(TerrakubeConnectionData)

	connection.Endpoint = endpoint
	connection.Token = token
	connection.InsecureHttpClient = insecureHttpClient

	resp.DataSourceData = connection
	resp.ResourceData = connection

	ctx = tflog.SetField(ctx, "terrakube_endpoint", endpoint)
	ctx = tflog.SetField(ctx, "terrakube_token", token)
	ctx = tflog.MaskFieldValuesWithFieldKeys(ctx, "terrakube_token")

	tflog.Info(ctx, "Creating Terrakube client information", map[string]any{"success": true})
}

func (p *TerrakubeProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewModuleResource,
		NewOrganizationResource,
		NewOrganizationTemplateResource,
		NewOrganizationTagResource,
		NewOrganizationVariableResource,
		NewTeamResource,
		NewTeamTokenResource,
		NewWorkspaceCliResource,
		NewWorkspaceTagResource,
		NewWorkspaceVariableResource,
		NewWorkspaceVcsResource,
		NewWorkspaceWebhookV2Resource,
		NewWorkspaceWebhookEventResource,
		NewVcsResource,
		NewWorkspaceScheduleResource,
		NewCollectionResource,
		NewCollectionItemResource,
		NewCollectionReferenceResource,
		NewAgentResource,
		NewWorkspaceAccessResource,
		NewSshResource,
	}
}

func (p *TerrakubeProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewOrganizationDataSource,
		NewOrganizationTemplateDataSource,
		NewOrganizationTagDataSource,
		NewVcsDataSource,
		NewSshDataSource,
		NewWorkspaceDataSource,
		NewOutputDataSource,
		NewTeamDataSource,
	}
}
