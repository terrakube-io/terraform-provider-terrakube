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
	_ datasource.DataSource              = &WorkspaceScheduleDataSource{}
	_ datasource.DataSourceWithConfigure = &WorkspaceScheduleDataSource{}
)

type WorkspaceScheduleDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	WorkspaceId types.String `tfsdk:"workspace_id"`
	TemplateId  types.String `tfsdk:"template_id"`
	Schedule    types.String `tfsdk:"schedule"`
}

type WorkspaceScheduleDataSource struct {
	client *terrakube.Client
}

func NewWorkspaceScheduleDataSource() datasource.DataSource {
	return &WorkspaceScheduleDataSource{}
}

func (d *WorkspaceScheduleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Workspace Schedule Data Source")
}

func (d *WorkspaceScheduleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_schedule"
}

func (d *WorkspaceScheduleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a workspace schedule by the template it runs. " +
			"When the workspace has more than one schedule for the same template, set `schedule` to pick one.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Schedule Id",
			},
			"workspace_id": schema.StringAttribute{
				Required:    true,
				Description: "Workspace Id",
			},
			"template_id": schema.StringAttribute{
				Required:    true,
				Description: "Template Id the schedule runs",
			},
			"schedule": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Schedule expression using java quartz notation. Set it to choose between schedules that run the same template.",
			},
		},
	}
}

func (d *WorkspaceScheduleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state WorkspaceScheduleDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceID := state.WorkspaceId.ValueString()
	templateID := state.TemplateId.ValueString()
	cron := state.Schedule.ValueString()
	filterByCron := !state.Schedule.IsNull() && !state.Schedule.IsUnknown()

	filter := "templateReference==" + rsqlQuote(templateID)
	if filterByCron {
		filter += ";cron==" + rsqlQuote(cron)
	}
	entries, err := d.client.WorkspaceSchedules.List(ctx, workspaceID, &terrakube.ListOptions{Filter: filter})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace schedule", err)
		return
	}

	// The API filters too; check again so a server that ignores the filter
	// cannot return the wrong schedule.
	var found *terrakube.WorkspaceSchedule
	for _, schedule := range entries {
		if schedule == nil || schedule.TemplateID != templateID || (filterByCron && schedule.Schedule != cron) {
			continue
		}
		if found != nil {
			resp.Diagnostics.AddError("Multiple workspace schedules found",
				fmt.Sprintf("More than one schedule for template %s was found in workspace %s. Set schedule to choose one.", templateID, workspaceID))
			return
		}
		found = schedule
	}
	if found == nil {
		detail := fmt.Sprintf("No schedule for template %s was found in workspace %s.", templateID, workspaceID)
		if filterByCron {
			detail = fmt.Sprintf("No schedule %q for template %s was found in workspace %s.", cron, templateID, workspaceID)
		}
		resp.Diagnostics.AddError("Workspace schedule not found", detail)
		return
	}

	state.ID = types.StringValue(found.ID)
	state.Schedule = types.StringValue(found.Schedule)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
