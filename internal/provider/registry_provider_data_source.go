package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	terrakube "github.com/terrakube-io/terrakube-go"
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
	client *terrakube.Client
}

func NewRegistryProviderDataSource() datasource.DataSource {
	return &RegistryProviderDataSource{}
}

func (d *RegistryProviderDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Registry Provider Data Source")
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
	providers, err := d.client.Providers.List(ctx, state.OrganizationId.ValueString(), &terrakube.ListOptions{Filter: "name==" + rsqlQuote(name)})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading registry provider", err)
		return
	}

	found := findSingleByNameIn(&resp.Diagnostics, "registry provider", name, " in organization "+state.OrganizationId.ValueString(), providers,
		func(e *terrakube.Provider) string { return e.Name })
	if found == nil {
		return
	}

	state.ID = types.StringValue(found.ID)
	state.Description = types.StringPointerValue(found.Description)
	state.Imported = types.BoolValue(found.Imported)
	// Un-mirroring a provider leaves an empty namespace behind. It reads as null, as in the resource.
	state.RegistryNamespace = stringFromAPI(types.StringNull(), found.RegistryNamespace)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
