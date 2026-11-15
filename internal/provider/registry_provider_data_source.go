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

func (d *RegistryProviderDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Registry Provider Data Source Configure Type",
			fmt.Sprintf("Expected *TerrakubeConnectionData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
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
	tflog.Debug(ctx, "Configured Registry Provider data source")
}

func (d *RegistryProviderDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_registry_provider"
}

func (d *RegistryProviderDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a provider in the private registry of a Terrakube organization by name.",
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
				Description: "Provider description, null when it has none",
			},
			"imported": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the provider is mirrored from the public registry. False on a Terrakube older than 2.30.0.",
			},
			"registry_namespace": schema.StringAttribute{
				Computed:    true,
				Description: "Namespace in the public registry of an imported provider, null otherwise",
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

	name := state.Name.ValueString()
	reqURL := fmt.Sprintf("%s/api/v1/organization/%s/provider?filter[provider]=name==%s",
		d.endpoint, url.PathEscape(state.OrganizationId.ValueString()), url.QueryEscape(rsqlQuote(name)))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating registry provider data source request", fmt.Sprintf("Error creating request: %s", err))
		return
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")

	response, err := d.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Error executing registry provider request", fmt.Sprintf("Error executing request: %s", err))
		return
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading registry provider response body", fmt.Sprintf("Error reading response body: %s", err))
		return
	}
	if reportAPIErrorStatus(&resp.Diagnostics, "reading registry provider", response, body) {
		return
	}

	providers, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(new(client.ProviderEntity)))
	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal registry provider payload", fmt.Sprintf("Unable to unmarshal payload: %s", err))
		return
	}

	found := findSingleByName(&resp.Diagnostics, "registry provider", name, " in organization "+state.OrganizationId.ValueString(), providers,
		func(e *client.ProviderEntity) string { return e.Name })
	if found == nil {
		return
	}

	state.ID = types.StringValue(found.ID)
	state.Description = types.StringPointerValue(found.Description)
	state.Imported = types.BoolValue(found.Imported != nil && *found.Imported)
	state.RegistryNamespace = types.StringPointerValue(found.RegistryNamespace)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
