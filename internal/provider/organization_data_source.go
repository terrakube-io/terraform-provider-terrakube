package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	terrakube "github.com/terrakube-io/terrakube-go"
)

var (
	_ datasource.DataSource              = &OrganizationDataSource{}
	_ datasource.DataSourceWithConfigure = &OrganizationDataSource{}
)

type OrganizationDataSourceModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Description   types.String `tfsdk:"description"`
	ExecutionMode types.String `tfsdk:"execution_mode"`
	Icon          types.String `tfsdk:"icon"`
}

type OrganizationDataSource struct {
	client *terrakube.Client
}

func NewOrganizationDataSource() datasource.DataSource {
	return &OrganizationDataSource{}
}

func (d *OrganizationDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Organization Data Source")
}

func (d *OrganizationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (d *OrganizationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an organization by name. Most other resources and data sources take the organization's `id` as `organization_id`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "ID of the organization.",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Name of the organization.",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Description of the organization.",
			},
			"execution_mode": schema.StringAttribute{
				Computed:    true,
				Description: "Default execution mode for the organization's workspaces: `remote` or `local`.",
			},
			"icon": schema.StringAttribute{
				Computed:    true,
				Description: "Icon of the organization, in the format `name:color`.",
			},
		},
	}
}

func (d *OrganizationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state OrganizationDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organizations, err := d.client.Organizations.List(ctx, &terrakube.ListOptions{Filter: "name==" + rsqlQuote(state.Name.ValueString())})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading organization", err)
		return
	}

	data := findSingleByNameIn(&resp.Diagnostics, "organization", state.Name.ValueString(), "", organizations,
		func(e *terrakube.Organization) string { return e.Name })
	if data == nil {
		return
	}
	// The data source has the same attributes as the resource, so it shares its mapping.
	state.ID = types.StringValue(data.ID)
	setOrganizationState((*OrganizationResourceModel)(&state), data)

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
