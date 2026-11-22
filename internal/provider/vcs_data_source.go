package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	terrakube "github.com/terrakube-io/terrakube-go"
)

var (
	_ datasource.DataSource              = &VcsDataSource{}
	_ datasource.DataSourceWithConfigure = &VcsDataSource{}
)

type VcsDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	ClientId       types.String `tfsdk:"client_id"`
	Endpoint       types.String `tfsdk:"endpoint"`
	ApiUrl         types.String `tfsdk:"api_url"`
	Status         types.String `tfsdk:"status"`
}

type VcsDataSource struct {
	client *terrakube.Client
}

func NewVcsDataSource() datasource.DataSource {
	return &VcsDataSource{}
}

func (d *VcsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Vcs Data Source")
}

func (d *VcsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vcs"
}

func (d *VcsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a VCS connection by name. Use `id` as a workspace's `vcs_id` to run it from a private repository. Secrets and private keys are never returned.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "ID of the VCS connection.",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "ID of the organization the VCS connection belongs to.",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Name of the VCS connection.",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Description of the VCS connection.",
			},
			"client_id": schema.StringAttribute{
				Computed:    true,
				Description: "Client ID, or GitHub App ID, of the VCS connection.",
			},
			"endpoint": schema.StringAttribute{
				Computed:    true,
				Description: "Endpoint of the VCS provider, for example `https://github.com`.",
			},
			"api_url": schema.StringAttribute{
				Computed:    true,
				Description: "API URL of the VCS provider, for example `https://api.github.com`.",
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "Status of the VCS connection. `PENDING` until the connection is authorized at its connect URL, then `COMPLETED`.",
			},
		},
	}
}

func (d *VcsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state VcsDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	connections, err := d.client.VCS.List(ctx, state.OrganizationId.ValueString(), &terrakube.ListOptions{Filter: "name==" + rsqlQuote(state.Name.ValueString())})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading vcs connection", err)
		return
	}

	data := findSingleByNameIn(&resp.Diagnostics, "vcs connection", state.Name.ValueString(), " in organization "+state.OrganizationId.ValueString(), connections,
		func(e *terrakube.VCS) string { return e.Name })
	if data == nil {
		return
	}
	// The client secret and private key are not part of the data source.
	state.ID = types.StringValue(data.ID)
	state.Description = types.StringValue(data.Description)
	state.ClientId = types.StringValue(data.ClientID)
	state.Endpoint = types.StringValue(data.Endpoint)
	state.ApiUrl = types.StringValue(data.APIURL)
	state.Status = types.StringValue(data.Status)

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
