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
	_ datasource.DataSource              = &NotificationConfigurationDataSource{}
	_ datasource.DataSourceWithConfigure = &NotificationConfigurationDataSource{}
)

type NotificationConfigurationDataSourceModel struct {
	ID              types.String `tfsdk:"id"`
	OrganizationId  types.String `tfsdk:"organization_id"`
	WorkspaceId     types.String `tfsdk:"workspace_id"`
	Name            types.String `tfsdk:"name"`
	Description     types.String `tfsdk:"description"`
	ChannelType     types.String `tfsdk:"channel_type"`
	DestinationUrl  types.String `tfsdk:"destination_url"`
	Active          types.Bool   `tfsdk:"active"`
	MessageStyle    types.String `tfsdk:"message_style"`
	TriggerStatuses types.List   `tfsdk:"trigger_statuses"`
	TemplateIds     types.List   `tfsdk:"template_ids"`
}

type NotificationConfigurationDataSource struct {
	client   *http.Client
	endpoint string
	token    string
}

func NewNotificationConfigurationDataSource() datasource.DataSource {
	return &NotificationConfigurationDataSource{}
}

func (d *NotificationConfigurationDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Notification Configuration Data Source Configure Type",
			fmt.Sprintf("Expected *TerrakubeConnectionData got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	if providerData.InsecureHttpClient {
		if custom, ok := http.DefaultTransport.(*http.Transport); ok {
			customTransport := custom.Clone()
			customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
			d.client = &http.Client{Transport: customTransport}
		} else {
			d.client = &http.Client{}
		}
	} else {
		d.client = &http.Client{}
	}
	d.endpoint = providerData.Endpoint
	d.token = providerData.Token

	tflog.Info(ctx, "Creating Notification Configuration datasource")
}

func (d *NotificationConfigurationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_notification_configuration"
}

func (d *NotificationConfigurationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a single notification configuration by name, within an organization and (optionally) a specific workspace. If `workspace_id` is omitted, looks up the organization-wide default; if set, looks up that workspace's override.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Notification configuration ID",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization id",
			},
			"workspace_id": schema.StringAttribute{
				Optional:    true,
				Description: "Terrakube workspace id. Omit to look up an organization-wide default; set to look up a workspace-level override.",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Notification configuration name",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Notification configuration description",
			},
			"channel_type": schema.StringAttribute{
				Computed:    true,
				Description: "Delivery channel: `SLACK`, `TEAMS`, or `WEBHOOK`.",
			},
			"destination_url": schema.StringAttribute{
				Computed:    true,
				Description: "Destination URL for the channel",
			},
			"active": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether this configuration is enabled",
			},
			"message_style": schema.StringAttribute{
				Computed:    true,
				Description: "Notification message format: `DETAILED` or `SIMPLE`",
			},
			"trigger_statuses": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Job statuses that trigger this notification",
			},
			"template_ids": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Template IDs this configuration is narrowed to. Empty means it applies to every template.",
			},
		},
	}
}

func (d *NotificationConfigurationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state NotificationConfigurationDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	reqURL := fmt.Sprintf("%s/api/v1/organization/%s/notificationConfiguration?filter[notification_configuration]=name==%s",
		d.endpoint, state.OrganizationId.ValueString(), url.QueryEscape(state.Name.ValueString()))
	request, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating notification configuration datasource request", fmt.Sprintf("Error creating notification configuration datasource request: %s", err))
		return
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")

	response, err := d.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Error executing notification configuration request", fmt.Sprintf("Error executing notification configuration request: %s", err))
		return
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading notification configuration response body", fmt.Sprintf("Error reading notification configuration response body: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(body)})

	list, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(new(client.NotificationConfigurationEntity)))
	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal payload", fmt.Sprintf("Unable to unmarshal payload: %s", err))
		return
	}

	wantWorkspaceScope := !state.WorkspaceId.IsNull() && state.WorkspaceId.ValueString() != ""
	var found *client.NotificationConfigurationEntity
	for _, item := range list {
		cfg := item.(*client.NotificationConfigurationEntity)
		hasWorkspace := cfg.Workspace != nil
		if wantWorkspaceScope {
			if hasWorkspace && cfg.Workspace.ID == state.WorkspaceId.ValueString() {
				found = cfg
				break
			}
		} else if !hasWorkspace {
			found = cfg
			break
		}
	}

	if found == nil {
		resp.Diagnostics.AddError("Notification configuration not found",
			fmt.Sprintf("No notification configuration found in organization %s with name %q for the requested scope", state.OrganizationId.ValueString(), state.Name.ValueString()))
		return
	}

	api := notificationConfigAPI{client: d.client, endpoint: d.endpoint, token: d.token}
	triggers, diags := api.fetchNotificationTriggers(ctx, found.ID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	statuses := make([]string, 0, len(triggers))
	for _, t := range triggers {
		statuses = append(statuses, t.JobStatus)
	}
	triggerList, listDiags := types.ListValueFrom(ctx, types.StringType, statuses)
	resp.Diagnostics.Append(listDiags...)

	templateIds, templateDiags := api.fetchNotificationConfigurationTemplateIDs(ctx, found.ID)
	resp.Diagnostics.Append(templateDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	templateList, templateListDiags := types.ListValueFrom(ctx, types.StringType, templateIds)
	resp.Diagnostics.Append(templateListDiags...)

	state.ID = types.StringValue(found.ID)
	state.Description = optionalStringValue(found.Description)
	state.ChannelType = types.StringValue(found.ChannelType)
	state.DestinationUrl = types.StringValue(found.DestinationUrl)
	state.Active = types.BoolValue(found.Active)
	state.MessageStyle = types.StringValue(found.MessageStyle)
	state.TriggerStatuses = triggerList
	state.TemplateIds = templateList

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
