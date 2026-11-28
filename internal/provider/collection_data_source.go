package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	terrakube "github.com/terrakube-io/terrakube-go"
)

var (
	_ datasource.DataSource              = &CollectionDataSource{}
	_ datasource.DataSourceWithConfigure = &CollectionDataSource{}
)

type CollectionDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	Priority       types.Int32  `tfsdk:"priority"`
}

type CollectionDataSource struct {
	client *terrakube.Client
}

func NewCollectionDataSource() datasource.DataSource {
	return &CollectionDataSource{}
}

func (d *CollectionDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Collection Data Source")
}

func (d *CollectionDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_collection"
}

func (d *CollectionDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a variable collection by name within a Terrakube organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Collection ID",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization ID",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Collection name",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Collection description, null when it has none",
			},
			"priority": schema.Int32Attribute{
				Computed:    true,
				Description: "Collection priority",
			},
		},
	}
}

func (d *CollectionDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state CollectionDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	collections, err := d.client.Collections.List(ctx, state.OrganizationId.ValueString(), &terrakube.ListOptions{Filter: "name==" + rsqlQuote(name)})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading collection", err)
		return
	}

	found := findSingleByNameIn(&resp.Diagnostics, "collection", name, " in organization "+state.OrganizationId.ValueString(), collections,
		func(e *terrakube.Collection) string { return e.Name })
	if found == nil {
		return
	}

	state.ID = types.StringValue(found.ID)
	state.Description = types.StringPointerValue(found.Description)
	state.Priority = types.Int32Value(found.Priority)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
