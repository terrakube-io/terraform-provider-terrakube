package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	terrakube "github.com/terrakube-io/terrakube-go"
)

var (
	_ datasource.DataSource              = &OrganizationTemplateDataSource{}
	_ datasource.DataSourceWithConfigure = &OrganizationTemplateDataSource{}
)

type OrganizationTemplateDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	OrganizationId types.String `tfsdk:"organization_id"`
}

type OrganizationTemplateDataSource struct {
	client *terrakube.Client
}

func NewOrganizationTemplateDataSource() datasource.DataSource {
	return &OrganizationTemplateDataSource{}
}

func (d *OrganizationTemplateDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Organization Template Data Source")
}

func (d *OrganizationTemplateDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_template"
}

func (d *OrganizationTemplateDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an organization template by name. Use `id` to set a workspace's default template, or to run the template on a schedule with `terrakube_workspace_schedule`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "ID of the template.",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Name of the template.",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "ID of the organization the template belongs to.",
			},
		},
	}
}

func (d *OrganizationTemplateDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state OrganizationTemplateDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	templates, err := d.client.Templates.List(ctx, state.OrganizationId.ValueString(), &terrakube.ListOptions{Filter: "name==" + rsqlQuote(state.Name.ValueString())})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading organization template", err)
		return
	}

	data := findSingleByNameIn(&resp.Diagnostics, "organization template", state.Name.ValueString(), " in organization "+state.OrganizationId.ValueString(), templates,
		func(e *terrakube.Template) string { return e.Name })
	if data == nil {
		return
	}
	state.ID = types.StringValue(data.ID)
	state.Name = types.StringValue(data.Name)

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
