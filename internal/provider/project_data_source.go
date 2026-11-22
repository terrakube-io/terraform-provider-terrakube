package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	terrakube "github.com/terrakube-io/terrakube-go"
)

var (
	_ datasource.DataSource              = &ProjectDataSource{}
	_ datasource.DataSourceWithConfigure = &ProjectDataSource{}
)

type ProjectDataSourceModel struct {
	ID           types.String `tfsdk:"id"`
	Organization types.String `tfsdk:"organization"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
}

type ProjectDataSource struct {
	client *terrakube.Client
}

func NewProjectDataSource() datasource.DataSource {
	return &ProjectDataSource{}
}

func (d *ProjectDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Project Data Source")
}

func (d *ProjectDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (d *ProjectDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a project by name. Unlike most data sources, it takes the organization's name, not its ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "ID of the project.",
			},
			"organization": schema.StringAttribute{
				Required:    true,
				Description: "Name of the organization the project belongs to.",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Name of the project.",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Description of the project.",
			},
		},
	}
}

func (d *ProjectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state ProjectDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("organization : %s", state.Organization.ValueString()))
	tflog.Info(ctx, fmt.Sprintf("project : %s", state.Name.ValueString()))

	projectName := state.Name.ValueString()

	organizations, err := d.client.Organizations.List(ctx, &terrakube.ListOptions{Filter: "name==" + rsqlQuote(state.Organization.ValueString())})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading project", err)
		return
	}
	organization := findSingleByNameIn(&resp.Diagnostics, "organization", state.Organization.ValueString(), "", organizations,
		func(e *terrakube.Organization) string { return e.Name })
	if organization == nil {
		return
	}

	projects, err := d.client.Projects.List(ctx, organization.ID, &terrakube.ListOptions{Filter: "name==" + rsqlQuote(projectName)})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading project", err)
		return
	}
	data := findSingleByNameIn(&resp.Diagnostics, "project", projectName, " in organization "+state.Organization.ValueString(), projects,
		func(e *terrakube.Project) string { return e.Name })
	if data == nil {
		return
	}
	state.ID = types.StringValue(data.ID)
	state.Name = types.StringValue(data.Name)
	state.Description = types.StringPointerValue(data.Description)

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
