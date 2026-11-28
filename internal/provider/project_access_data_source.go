package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	terrakube "github.com/terrakube-io/terrakube-go"
)

var (
	_ datasource.DataSource              = &ProjectAccessDataSource{}
	_ datasource.DataSourceWithConfigure = &ProjectAccessDataSource{}
)

type ProjectAccessDataSourceModel struct {
	ID              types.String `tfsdk:"id"`
	OrganizationId  types.String `tfsdk:"organization_id"`
	ProjectId       types.String `tfsdk:"project_id"`
	Name            types.String `tfsdk:"name"`
	ManageState     types.Bool   `tfsdk:"manage_state"`
	ManageJob       types.Bool   `tfsdk:"manage_job"`
	ManageWorkspace types.Bool   `tfsdk:"manage_workspace"`
	PlanJob         types.Bool   `tfsdk:"plan_job"`
	ApproveJob      types.Bool   `tfsdk:"approve_job"`
	DestroyJob      types.Bool   `tfsdk:"destroy_job"`
	Role            types.String `tfsdk:"role"`
}

type ProjectAccessDataSource struct {
	client *terrakube.Client
}

func NewProjectAccessDataSource() datasource.DataSource {
	return &ProjectAccessDataSource{}
}

func (d *ProjectAccessDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Project Access Data Source")
}

func (d *ProjectAccessDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_access"
}

func (d *ProjectAccessDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up the access a team has to a project by team name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Access ID",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization ID",
			},
			"project_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube project ID",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Team name",
			},
			"manage_state": schema.BoolAttribute{
				Computed:    true,
				Description: "Allowed to manage Terraform/OpenTofu state",
			},
			"manage_job": schema.BoolAttribute{
				Computed:    true,
				Description: "Allowed to manage and trigger jobs",
			},
			"manage_workspace": schema.BoolAttribute{
				Computed:    true,
				Description: "Allowed to manage workspaces",
			},
			"plan_job": schema.BoolAttribute{
				Computed:    true,
				Description: "Allowed to queue plans. Only used when role is unset or custom.",
			},
			"approve_job": schema.BoolAttribute{
				Computed:    true,
				Description: "Allowed to approve and apply runs. Only used when role is unset or custom.",
			},
			"destroy_job": schema.BoolAttribute{
				Computed:    true,
				Description: "Allowed to run destroy (Terrakube 2.34.0+). Null when the API does not return it.",
			},
			"role": schema.StringAttribute{
				Computed:    true,
				Description: "Predefined role: admin, write, plan, read or custom. Null when unset.",
			},
		},
	}
}

func (d *ProjectAccessDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state ProjectAccessDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	entries, err := d.client.ProjectAccess.List(ctx, state.OrganizationId.ValueString(), state.ProjectId.ValueString(), &terrakube.ListOptions{Filter: "name==" + rsqlQuote(name)})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading project access", err)
		return
	}

	var found *terrakube.ProjectAccess
	for _, access := range entries {
		if access == nil || access.Name != name {
			continue
		}
		if found != nil {
			resp.Diagnostics.AddError("Multiple project access entries found",
				fmt.Sprintf("More than one entry for team %q was found in project %s.", name, state.ProjectId.ValueString()))
			return
		}
		found = access
	}
	if found == nil {
		resp.Diagnostics.AddError("Project access not found",
			fmt.Sprintf("No access for team %q was found in project %s.", name, state.ProjectId.ValueString()))
		return
	}

	state.ID = types.StringValue(found.ID)
	state.ManageState = types.BoolValue(found.ManageState)
	state.ManageJob = types.BoolValue(found.ManageJob)
	state.ManageWorkspace = types.BoolValue(found.ManageWorkspace)
	state.PlanJob = types.BoolValue(found.PlanJob)
	state.ApproveJob = types.BoolValue(found.ApproveJob)
	state.DestroyJob = types.BoolPointerValue(found.DestroyJob)
	state.Role = roleToState(&found.Role)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
