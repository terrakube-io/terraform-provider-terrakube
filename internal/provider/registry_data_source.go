package provider

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
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
	_ datasource.DataSource              = &RegistryDataSource{}
	_ datasource.DataSourceWithConfigure = &RegistryDataSource{}
)

type RegistryModuleSummary struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	ProviderName  types.String `tfsdk:"provider_name"`
	Description   types.String `tfsdk:"description"`
	Source        types.String `tfsdk:"source"`
	RegistryPath  types.String `tfsdk:"registry_path"`
	LatestVersion types.String `tfsdk:"latest_version"`
}

type RegistryProviderSummary struct {
	ID                types.String `tfsdk:"id"`
	Name              types.String `tfsdk:"name"`
	Description       types.String `tfsdk:"description"`
	Imported          types.Bool   `tfsdk:"imported"`
	RegistryNamespace types.String `tfsdk:"registry_namespace"`
}

type RegistryDataSourceModel struct {
	OrganizationId types.String              `tfsdk:"organization_id"`
	Modules        []RegistryModuleSummary   `tfsdk:"modules"`
	Providers      []RegistryProviderSummary `tfsdk:"providers"`
}

type RegistryDataSource struct {
	client   *http.Client
	endpoint string
	token    string
}

func NewRegistryDataSource() datasource.DataSource {
	return &RegistryDataSource{}
}

func (d *RegistryDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, res *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		res.Diagnostics.AddError(
			"Unexpected Registry Data Source Configure Type",
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

	tflog.Info(ctx, "Creating Registry datasource")
}

func (d *RegistryDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_registry"
}

func (d *RegistryDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads the contents of the Terrakube private registry for an organization: " +
			"the full list of published modules and providers. Each organization in Terrakube has its own registry.",
		Attributes: map[string]schema.Attribute{
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization ID whose registry should be read",
			},
			"modules": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Modules published in this organization's registry",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":             schema.StringAttribute{Computed: true, Description: "Module ID"},
						"name":           schema.StringAttribute{Computed: true, Description: "Module name"},
						"provider_name":  schema.StringAttribute{Computed: true, Description: "Module provider name"},
						"description":    schema.StringAttribute{Computed: true, Description: "Module description"},
						"source":         schema.StringAttribute{Computed: true, Description: "Source Git URL"},
						"registry_path":  schema.StringAttribute{Computed: true, Description: "Registry path `{organization}/{name}/{provider}`"},
						"latest_version": schema.StringAttribute{Computed: true, Description: "Latest version detected by Terrakube's VCS scan"},
					},
				},
			},
			"providers": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Providers published in this organization's registry",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":                 schema.StringAttribute{Computed: true, Description: "Provider ID"},
						"name":               schema.StringAttribute{Computed: true, Description: "Provider name"},
						"description":        schema.StringAttribute{Computed: true, Description: "Provider description"},
						"imported":           schema.BoolAttribute{Computed: true, Description: "Whether this provider mirrors an upstream public one"},
						"registry_namespace": schema.StringAttribute{Computed: true, Description: "Upstream registry namespace for imported providers"},
					},
				},
			},
		},
	}
}

func (d *RegistryDataSource) listModules(ctx context.Context, orgID string) ([]*client.ModuleEntity, error) {
	endpoint := fmt.Sprintf("%s/api/v1/organization/%s/module", d.endpoint, orgID)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	req.Header.Add("Content-Type", "application/vnd.api+json")

	res, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("status: %s, body: %s", res.Status, string(body))
	}

	raw, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(new(client.ModuleEntity)))
	if err != nil {
		return nil, err
	}
	out := make([]*client.ModuleEntity, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(*client.ModuleEntity); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

func (d *RegistryDataSource) listProviders(ctx context.Context, orgID string) ([]*client.ProviderEntity, error) {
	endpoint := fmt.Sprintf("%s/api/v1/organization/%s/provider", d.endpoint, orgID)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	req.Header.Add("Content-Type", "application/vnd.api+json")

	res, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("status: %s, body: %s", res.Status, string(body))
	}

	raw, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(new(client.ProviderEntity)))
	if err != nil {
		return nil, err
	}
	out := make([]*client.ProviderEntity, 0, len(raw))
	for _, item := range raw {
		if p, ok := item.(*client.ProviderEntity); ok {
			out = append(out, p)
		}
	}
	return out, nil
}

func (d *RegistryDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state RegistryDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	orgID := state.OrganizationId.ValueString()

	modules, err := d.listModules(ctx, orgID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list modules", err.Error())
		return
	}

	providers, err := d.listProviders(ctx, orgID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list providers", err.Error())
		return
	}

	state.Modules = make([]RegistryModuleSummary, 0, len(modules))
	for _, m := range modules {
		state.Modules = append(state.Modules, RegistryModuleSummary{
			ID:            types.StringValue(m.ID),
			Name:          types.StringValue(m.Name),
			ProviderName:  types.StringValue(m.Provider),
			Description:   types.StringValue(m.Description),
			Source:        types.StringValue(m.Source),
			RegistryPath:  stringValueOrNull(m.RegistryPath),
			LatestVersion: stringValueOrNull(m.LatestVersion),
		})
	}

	state.Providers = make([]RegistryProviderSummary, 0, len(providers))
	for _, p := range providers {
		state.Providers = append(state.Providers, RegistryProviderSummary{
			ID:                types.StringValue(p.ID),
			Name:              types.StringValue(p.Name),
			Description:       stringValueOrNull(p.Description),
			Imported:          types.BoolValue(p.Imported),
			RegistryNamespace: stringValueOrNull(p.RegistryNamespace),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func stringValueOrNull(v string) types.String {
	if v == "" {
		return types.StringNull()
	}
	return types.StringValue(v)
}
