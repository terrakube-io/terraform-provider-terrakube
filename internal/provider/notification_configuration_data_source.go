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
	client *terrakube.Client
}

func NewNotificationConfigurationDataSource() datasource.DataSource {
	return &NotificationConfigurationDataSource{}
}

func (d *NotificationConfigurationDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Notification Configuration Data Source")
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

	list, err := d.client.NotificationConfigurations.List(ctx, state.OrganizationId.ValueString(), &terrakube.ListOptions{Filter: "name==" + state.Name.ValueString()})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading notification configuration", err)
		return
	}

	wantWorkspaceScope := !state.WorkspaceId.IsNull() && state.WorkspaceId.ValueString() != ""
	var found *terrakube.NotificationConfiguration
	for _, cfg := range list {
		if cfg == nil {
			continue
		}
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

	triggers, err := d.client.NotificationTriggers.ListByConfiguration(ctx, found.ID, nil)
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading notification triggers", err)
		return
	}
	statuses := make([]string, 0, len(triggers))
	for _, t := range triggers {
		statuses = append(statuses, t.JobStatus)
	}
	triggerList, listDiags := types.ListValueFrom(ctx, types.StringType, statuses)
	resp.Diagnostics.Append(listDiags...)

	templateList, templateListDiags := types.ListValueFrom(ctx, types.StringType, notificationTemplateIDs(found))
	resp.Diagnostics.Append(templateListDiags...)

	state.ID = types.StringValue(found.ID)
	state.Description = optionalStringValue(found.Description)
	state.ChannelType = types.StringValue(found.ChannelType)
	state.DestinationUrl = types.StringValue(found.DestinationURL)
	state.Active = types.BoolValue(found.Active)
	state.MessageStyle = types.StringValue(found.MessageStyle)
	state.TriggerStatuses = triggerList
	state.TemplateIds = templateList

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
