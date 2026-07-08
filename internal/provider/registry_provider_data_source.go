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
	_ datasource.DataSource              = &RegistryProviderDataSource{}
	_ datasource.DataSourceWithConfigure = &RegistryProviderDataSource{}
)

type RegistryProviderDataSourceModel struct {
	ID                types.String `tfsdk:"id"`
	OrganizationId    types.String `tfsdk:"organization_id"`
	Name              types.String `tfsdk:"name"`
	Description       types.String `tfsdk:"description"`
	Imported          types.Bool   `tfsdk:"imported"`
	RegistryNamespace types.String `tfsdk:"registry_namespace"`
}

type RegistryProviderDataSource struct {
	client   *http.Client
	endpoint string
	token    string
}

func NewRegistryProviderDataSource() datasource.DataSource {
	return &RegistryProviderDataSource{}
}

func (d *RegistryProviderDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, res *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		res.Diagnostics.AddError(
			"Unexpected Registry Provider Data Source Configure Type",
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

	tflog.Info(ctx, "Creating Registry Provider datasource")
}

func (d *RegistryProviderDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_registry_provider"
}

func (d *RegistryProviderDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a provider published in the organization's Terrakube registry by name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Provider ID",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization ID",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Provider name",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Provider description",
			},
			"imported": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the provider mirrors an upstream public provider",
			},
			"registry_namespace": schema.StringAttribute{
				Computed:    true,
				Description: "Upstream registry namespace for imported providers (e.g. `hashicorp`)",
			},
		},
	}
}

func (d *RegistryProviderDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state RegistryProviderDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	filter := url.QueryEscape(fmt.Sprintf("name==%s", state.Name.ValueString()))
	endpoint := fmt.Sprintf("%s/api/v1/organization/%s/provider?filter[provider]=%s", d.endpoint, state.OrganizationId.ValueString(), filter)

	providerRequest, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating registry provider data source request", fmt.Sprintf("Error creating registry provider data source request: %s", err))
		return
	}
	providerRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	providerRequest.Header.Add("Content-Type", "application/vnd.api+json")

	providerResponse, err := d.client.Do(providerRequest)
	if err != nil {
		resp.Diagnostics.AddError("Request errored", fmt.Sprintf("error: %v", err))
		return
	}

	body, err := io.ReadAll(providerResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading body", fmt.Sprintf("status: %v, error: %v", providerResponse.Status, err))
		return
	}
	if providerResponse.StatusCode >= 400 {
		resp.Diagnostics.AddError("Request failed", fmt.Sprintf("status: %v, body: %v", providerResponse.Status, string(body)))
		return
	}

	providers, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(new(client.ProviderEntity)))
	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal payload", fmt.Sprintf("status: %s, body: %s, error: %s", providerResponse.Status, string(body), err))
		return
	}

	if len(providers) == 0 {
		resp.Diagnostics.AddError(
			"Provider not found",
			fmt.Sprintf("No provider named %q was found in organization %s", state.Name.ValueString(), state.OrganizationId.ValueString()),
		)
		return
	}

	provider, _ := providers[0].(*client.ProviderEntity)
	state.ID = types.StringValue(provider.ID)
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
