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
	_ datasource.DataSource              = &WorkspaceAccessDataSource{}
	_ datasource.DataSourceWithConfigure = &WorkspaceAccessDataSource{}
)

type WorkspaceAccessDataSourceModel struct {
	ID              types.String `tfsdk:"id"`
	OrganizationId  types.String `tfsdk:"organization_id"`
	WorkspaceId     types.String `tfsdk:"workspace_id"`
	Name            types.String `tfsdk:"name"`
	ManageState     types.Bool   `tfsdk:"manage_state"`
	ManageJob       types.Bool   `tfsdk:"manage_job"`
	ManageWorkspace types.Bool   `tfsdk:"manage_workspace"`
	PlanJob         types.Bool   `tfsdk:"plan_job"`
	ApproveJob      types.Bool   `tfsdk:"approve_job"`
	DestroyJob      types.Bool   `tfsdk:"destroy_job"`
	Role            types.String `tfsdk:"role"`
}

type WorkspaceAccessDataSource struct {
	client *terrakube.Client
}

func NewWorkspaceAccessDataSource() datasource.DataSource {
	return &WorkspaceAccessDataSource{}
}

func (d *WorkspaceAccessDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Workspace Access Data Source")
}

func (d *WorkspaceAccessDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_access"
}

func (d *WorkspaceAccessDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up the access a team has to a workspace by team name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Access ID",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization ID",
			},
			"workspace_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube workspace ID",
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

func (d *WorkspaceAccessDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state WorkspaceAccessDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	entries, err := d.client.WorkspaceAccess.List(ctx, state.OrganizationId.ValueString(), state.WorkspaceId.ValueString(), &terrakube.ListOptions{Filter: "name==" + rsqlQuote(name)})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace access", err)
		return
	}

	var found *terrakube.WorkspaceAccess
	for _, access := range entries {
		if access == nil || access.Name != name {
			continue
		}
		if found != nil {
			resp.Diagnostics.AddError("Multiple workspace access entries found",
				fmt.Sprintf("More than one entry for team %q was found in workspace %s.", name, state.WorkspaceId.ValueString()))
			return
		}
		found = access
	}
	if found == nil {
		resp.Diagnostics.AddError("Workspace access not found",
			fmt.Sprintf("No access for team %q was found in workspace %s.", name, state.WorkspaceId.ValueString()))
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
