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
	_ datasource.DataSource              = &TeamDataSource{}
	_ datasource.DataSourceWithConfigure = &TeamDataSource{}
)

type TeamDataSourceModel struct {
	Role             types.String `tfsdk:"role"`
	PlanJob          types.Bool   `tfsdk:"plan_job"`
	ApproveJob       types.Bool   `tfsdk:"approve_job"`
	DestroyJob       types.Bool   `tfsdk:"destroy_job"`
	Organization     types.String `tfsdk:"organization"`
	Name             types.String `tfsdk:"name"`
	ManageCollection types.Bool   `tfsdk:"manage_collection"`
	ManageJob        types.Bool   `tfsdk:"manage_job"`
	ManageModule     types.Bool   `tfsdk:"manage_module"`
	ManageProvider   types.Bool   `tfsdk:"manage_provider"`
	ManageState      types.Bool   `tfsdk:"manage_state"`
	ManageTemplate   types.Bool   `tfsdk:"manage_template"`
	ManageVcs        types.Bool   `tfsdk:"manage_vcs"`
	ManageWorkspace  types.Bool   `tfsdk:"manage_workspace"`
	ManagePolicies   types.Bool   `tfsdk:"manage_policies"`
}

type TeamDataSource struct {
	client *terrakube.Client
}

func NewTeamDataSource() datasource.DataSource {
	return &TeamDataSource{}
}

func (d *TeamDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Team Data Source")
}

func (d *TeamDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_team"
}

func (d *TeamDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a team by name and reads its permissions in the organization. Unlike most data sources, it takes the organization's name, not its ID.",
		Attributes: map[string]schema.Attribute{
			"role":        schema.StringAttribute{Computed: true, Description: "Predefined role of the team: `admin`, `write`, `plan`, `read` or `custom`. Null when the team uses only the permission flags."},
			"plan_job":    schema.BoolAttribute{Computed: true, Description: "Whether the team can queue plans."},
			"approve_job": schema.BoolAttribute{Computed: true, Description: "Whether the team can approve and apply runs."},
			"destroy_job": schema.BoolAttribute{Computed: true, Description: "Whether the team can start destroy runs, when `role` is `custom` or not set (Terrakube 2.34.0+)."},
			"organization": schema.StringAttribute{
				Required:    true,
				Description: "Name of the organization the team belongs to.",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Name of the team. For a team that maps to an identity provider group, this is the group name.",
			},
			"manage_collection": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the team can manage variable collections.",
			},
			"manage_job": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the team can manage and start jobs. In RBAC v2, `plan_job` and `approve_job` inherit this when they are not set.",
			},
			"manage_module": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the team can manage private registry modules.",
			},
			"manage_provider": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the team can manage private registry providers.",
			},
			"manage_state": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the team can manage Terraform/OpenTofu state.",
			},
			"manage_template": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the team can manage templates.",
			},
			"manage_vcs": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the team can manage VCS connections.",
			},
			"manage_workspace": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the team can manage workspaces.",
			},
			"manage_policies": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the team can manage OPA policy sets, attachments and exemptions.",
			},
		},
	}
}

func (d *TeamDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state TeamDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, fmt.Sprintf("organization : %s", state.Organization.ValueString()))
	tflog.Info(ctx, fmt.Sprintf("team : %s", state.Name.ValueString()))

	teamName := state.Name.ValueString()

	organizations, err := d.client.Organizations.List(ctx, &terrakube.ListOptions{Filter: "name==" + rsqlQuote(state.Organization.ValueString())})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading team", err)
		return
	}
	organization := findSingleByNameIn(&resp.Diagnostics, "organization", state.Organization.ValueString(), "", organizations,
		func(e *terrakube.Organization) string { return e.Name })
	if organization == nil {
		return
	}

	teams, err := d.client.Teams.List(ctx, organization.ID, &terrakube.ListOptions{Filter: "name==" + rsqlQuote(teamName)})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading team", err)
		return
	}
	data := findSingleByNameIn(&resp.Diagnostics, "team", teamName, " in organization "+state.Organization.ValueString(), teams,
		func(e *terrakube.Team) string { return e.Name })
	if data == nil {
		return
	}
	state.Role = roleToState(&data.Role)
	state.PlanJob = types.BoolValue(data.PlanJob)
	state.ApproveJob = types.BoolValue(data.ApproveJob)
	state.DestroyJob = types.BoolPointerValue(data.DestroyJob)
	state.ManageCollection = types.BoolValue(data.ManageCollection)
	state.ManageJob = types.BoolValue(data.ManageJob)
	state.ManageModule = types.BoolValue(data.ManageModule)
	state.ManageProvider = types.BoolValue(data.ManageProvider)
	state.ManageState = types.BoolValue(data.ManageState)
	state.ManageTemplate = types.BoolValue(data.ManageTemplate)
	state.ManageVcs = types.BoolValue(data.ManageVcs)
	state.ManageWorkspace = types.BoolValue(data.ManageWorkspace)
	state.ManagePolicies = types.BoolPointerValue(data.ManagePolicies)

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
