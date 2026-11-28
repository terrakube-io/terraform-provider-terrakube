package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	terrakube "github.com/terrakube-io/terrakube-go"
)

var (
	_ datasource.DataSource              = &OrganizationTagDataSource{}
	_ datasource.DataSourceWithConfigure = &OrganizationTagDataSource{}
)

type OrganizationTagDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	OrganizationId types.String `tfsdk:"organization_id"`
}

type OrganizationTagDataSource struct {
	client *terrakube.Client
}

func NewOrganizationTagDataSource() datasource.DataSource {
	return &OrganizationTagDataSource{}
}

func (d *OrganizationTagDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Organization Tag Data Source")
}

func (d *OrganizationTagDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_tag"
}

func (d *OrganizationTagDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an organization tag by name. Use `id` to attach the tag to a workspace with `terrakube_workspace_tag`, or to attach a policy set to every workspace with the tag.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "ID of the tag.",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "ID of the organization the tag belongs to.",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Name of the tag.",
			},
		},
	}
}

func (d *OrganizationTagDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state OrganizationTagDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tags, err := d.client.Tags.List(ctx, state.OrganizationId.ValueString(), &terrakube.ListOptions{Filter: "name==" + rsqlQuote(state.Name.ValueString())})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading organization tag", err)
		return
	}

	data := findSingleByNameIn(&resp.Diagnostics, "organization tag", state.Name.ValueString(), " in organization "+state.OrganizationId.ValueString(), tags,
		func(e *terrakube.Tag) string { return e.Name })
	if data == nil {
		return
	}
	state.ID = types.StringValue(data.ID)
	state.Name = types.StringValue(data.Name)

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
