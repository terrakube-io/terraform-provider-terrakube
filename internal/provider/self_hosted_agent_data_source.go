package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	terrakube "github.com/terrakube-io/terrakube-go"
)

var (
	_ datasource.DataSource              = &SelfHostedAgentDataSource{}
	_ datasource.DataSourceWithConfigure = &SelfHostedAgentDataSource{}
)

type SelfHostedAgentDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	Url            types.String `tfsdk:"url"`
}

type SelfHostedAgentDataSource struct {
	client *terrakube.Client
}

func NewSelfHostedAgentDataSource() datasource.DataSource {
	return &SelfHostedAgentDataSource{}
}

func (d *SelfHostedAgentDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Self Hosted Agent Data Source")
}

func (d *SelfHostedAgentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_self_hosted_agent"
}

func (d *SelfHostedAgentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a self-hosted agent by name within a Terrakube organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Self-hosted agent ID",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization ID",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Self-hosted agent name",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Description of the self-hosted agent",
			},
			"url": schema.StringAttribute{
				Computed:    true,
				Description: "URL of the self-hosted agent",
			},
		},
	}
}

func (d *SelfHostedAgentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state SelfHostedAgentDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	agents, err := d.client.Agents.List(ctx, state.OrganizationId.ValueString(), &terrakube.ListOptions{Filter: "name==" + rsqlQuote(state.Name.ValueString())})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading self-hosted agent", err)
		return
	}

	found := findSingleByNameIn(&resp.Diagnostics, "self-hosted agent", state.Name.ValueString(), " in organization "+state.OrganizationId.ValueString(), agents,
		func(e *terrakube.Agent) string { return e.Name })
	if found == nil {
		return
	}

	state.ID = types.StringValue(found.ID)
	state.Description = types.StringValue(found.Description)
	state.Url = types.StringValue(found.URL)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
