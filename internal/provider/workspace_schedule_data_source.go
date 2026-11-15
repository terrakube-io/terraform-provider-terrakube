package provider

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"terraform-provider-terrakube/internal/client"

	"github.com/google/jsonapi"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
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
	client   *http.Client
	endpoint string
	token    string
}

func NewWorkspaceScheduleDataSource() datasource.DataSource {
	return &WorkspaceScheduleDataSource{}
}

func (d *WorkspaceScheduleDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Workspace Schedule Data Source Configure Type",
			fmt.Sprintf("Expected *TerrakubeConnectionData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	d.client = &http.Client{}
	if providerData.InsecureHttpClient {
		if custom, ok := http.DefaultTransport.(*http.Transport); ok {
			customTransport := custom.Clone()
			customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
			d.client = &http.Client{Transport: customTransport}
		}
	}
	d.endpoint = providerData.Endpoint
	d.token = providerData.Token
	tflog.Debug(ctx, "Configured Workspace Schedule data source")
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
	reqURL := fmt.Sprintf("%s/api/v1/workspace/%s/schedule?filter[schedule]=%s",
		d.endpoint, url.PathEscape(workspaceID), url.QueryEscape(filter))

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace schedule data source request", fmt.Sprintf("Error creating request: %s", err))
		return
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")

	response, err := d.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace schedule request", fmt.Sprintf("Error executing request: %s", err))
		return
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading workspace schedule response body", fmt.Sprintf("Error reading response body: %s", err))
		return
	}
	if reportAPIErrorStatus(&resp.Diagnostics, "reading workspace schedule", response, body) {
		return
	}

	entries, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(new(client.WorkspaceScheduleEntity)))
	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal workspace schedule payload", fmt.Sprintf("Unable to unmarshal payload: %s", err))
		return
	}

	// The API filters too; check again so a server that ignores the filter
	// cannot return the wrong schedule.
	var found *client.WorkspaceScheduleEntity
	for _, item := range entries {
		schedule, ok := item.(*client.WorkspaceScheduleEntity)
		if !ok || schedule == nil {
			resp.Diagnostics.AddError("Unable to unmarshal workspace schedule payload", "Unexpected type in workspace schedule payload.")
			return
		}
		if schedule.TemplateId != templateID || (filterByCron && schedule.Schedule != cron) {
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
