package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	terrakube "github.com/terrakube-io/terrakube-go"
)

var _ resource.Resource = &OrganizationNotificationConfigurationResource{}
var _ resource.ResourceWithImportState = &OrganizationNotificationConfigurationResource{}
var _ resource.ResourceWithConfigValidators = &OrganizationNotificationConfigurationResource{}

var notificationJobStatusValues = []string{
	"pending", "waitingApproval", "approved", "queue", "running",
	"completed", "noChanges", "notExecuted", "rejected", "cancelled",
	"failed", "unknown", "NeverExecuted",
}

type OrganizationNotificationConfigurationResource struct {
	client *terrakube.Client
}

type OrganizationNotificationConfigurationResourceModel struct {
	ID                     types.String `tfsdk:"id"`
	OrganizationId         types.String `tfsdk:"organization_id"`
	Name                   types.String `tfsdk:"name"`
	Description            types.String `tfsdk:"description"`
	ChannelType            types.String `tfsdk:"channel_type"`
	DestinationUrl         types.String `tfsdk:"destination_url"`
	SigningSecret          types.String `tfsdk:"signing_secret"`
	SigningSecretWo        types.String `tfsdk:"signing_secret_wo"`
	SigningSecretWoVersion types.Int64  `tfsdk:"signing_secret_wo_version"`
	Active                 types.Bool   `tfsdk:"active"`
	MessageStyle           types.String `tfsdk:"message_style"`
	TriggerStatuses        types.List   `tfsdk:"trigger_statuses"`
	TemplateIds            types.List   `tfsdk:"template_ids"`
}

func NewOrganizationNotificationConfigurationResource() resource.Resource {
	return &OrganizationNotificationConfigurationResource{}
}

func (r *OrganizationNotificationConfigurationResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_notification_configuration"
}

func (r *OrganizationNotificationConfigurationResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create an organization-wide default notification configuration (Slack, Teams, or a generic webhook) that fires for every workspace in the organization, for the selected job status triggers. This is purely additive alongside any `terrakube_workspace_notification_configuration` in the same organization - there is no override or suppression between the two scopes; a workspace with its own configuration still also receives this one.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Notification configuration ID",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization id",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Notification configuration name",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Notification configuration description",
			},
			"channel_type": schema.StringAttribute{
				Required:    true,
				Description: "Delivery channel. Valid values: `SLACK`, `TEAMS`, `WEBHOOK`.",
				Validators: []validator.String{
					stringvalidator.OneOf("SLACK", "TEAMS", "WEBHOOK"),
				},
			},
			"destination_url": schema.StringAttribute{
				Required:    true,
				Description: "Destination URL for the channel (Slack/Teams incoming webhook URL, or the target URL for a generic webhook).",
			},
			"signing_secret": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: signingSecretDescription,
			},
			"signing_secret_wo":         writeOnlyAttribute("signing_secret", "Webhook signing secret", false),
			"signing_secret_wo_version": writeOnlyVersionAttribute("signing_secret"),
			"active": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether this configuration is enabled. An inactive configuration never fires.",
			},
			"message_style": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("DETAILED"),
				Description: "Notification message format. `DETAILED` renders the full card (org/job/commit, view-run link, sent-by footer). `SIMPLE` renders a single-line ping. Valid values: `DETAILED`, `SIMPLE`.",
				Validators: []validator.String{
					stringvalidator.OneOf("DETAILED", "SIMPLE"),
				},
			},
			"trigger_statuses": schema.ListAttribute{
				Required:    true,
				ElementType: types.StringType,
				Description: "Job statuses that trigger this notification. Valid values: `pending`, `waitingApproval`, `approved`, `queue`, `running`, `completed`, `noChanges`, `notExecuted`, `rejected`, `cancelled`, `failed`, `unknown`, `NeverExecuted`.",
				Validators: []validator.List{
					listvalidator.ValueStringsAre(stringvalidator.OneOf(notificationJobStatusValues...)),
				},
			},
			"template_ids": schema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Default:     listdefault.StaticValue(types.ListValueMust(types.StringType, []attr.Value{})),
				Description: "Template IDs this configuration is narrowed to. Empty (the default) means it applies to every template - this list only ever narrows, it never widens beyond that.",
			},
		},
	}
}

func (r *OrganizationNotificationConfigurationResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Organization Notification Configuration Resource")
}

func (r *OrganizationNotificationConfigurationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan OrganizationNotificationConfigurationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var triggerStatuses []string
	resp.Diagnostics.Append(plan.TriggerStatuses.ElementsAs(ctx, &triggerStatuses, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var templateIds []string
	if !plan.TemplateIds.IsNull() && !plan.TemplateIds.IsUnknown() {
		resp.Diagnostics.Append(plan.TemplateIds.ElementsAs(ctx, &templateIds, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	signingSecret, secretDiags := signingSecretToSend(ctx, req.Config, plan.SigningSecret)
	resp.Diagnostics.Append(secretDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.createString("name", plan.Name)
	attributes.createString("description", plan.Description)
	attributes.createString("channelType", plan.ChannelType)
	attributes.createString("destinationUrl", plan.DestinationUrl)
	if signingSecret != nil {
		attributes["signingSecret"] = *signingSecret
	}
	attributes.createBool("active", plan.Active)
	attributes.createString("messageStyle", plan.MessageStyle)

	created, err := r.client.NotificationConfigurations.CreateFields(ctx, plan.OrganizationId.ValueString(), terrakube.Patch{
		Attributes:    attributes,
		Relationships: notificationTemplatesRelationship(templateIds),
	})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating organization notification configuration", err)
		return
	}

	resp.Diagnostics.Append(syncNotificationTriggers(ctx, r.client, created.ID, nil, triggerStatuses)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = types.StringValue(created.ID)
	plan.Name = types.StringValue(created.Name)
	plan.Description = optionalStringValue(created.Description)
	plan.ChannelType = types.StringValue(created.ChannelType)
	plan.DestinationUrl = types.StringValue(created.DestinationURL)
	// signing_secret stays as planned: Terrakube never returns it (see the Read comment).
	plan.Active = types.BoolValue(created.Active)
	plan.MessageStyle = types.StringValue(created.MessageStyle)
	triggerList, listDiags := types.ListValueFrom(ctx, types.StringType, triggerStatuses)
	resp.Diagnostics.Append(listDiags...)
	plan.TriggerStatuses = triggerList
	templateList, templateListDiags := types.ListValueFrom(ctx, types.StringType, templateIds)
	resp.Diagnostics.Append(templateListDiags...)
	plan.TemplateIds = templateList

	tflog.Info(ctx, "Organization Notification Configuration Resource Created", map[string]any{"success": true})
	plan.SigningSecretWo = types.StringNull()
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *OrganizationNotificationConfigurationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state OrganizationNotificationConfigurationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	configuration, err := r.client.NotificationConfigurations.GetByID(ctx, state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Notification configuration not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading organization notification configuration", err)
		return
	}

	triggers, err := r.client.NotificationTriggers.ListByConfiguration(ctx, state.ID.ValueString(), nil)
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

	templateList, templateListDiags := types.ListValueFrom(ctx, types.StringType, notificationTemplateIDs(configuration))
	resp.Diagnostics.Append(templateListDiags...)

	state.ID = types.StringValue(configuration.ID)
	state.Name = types.StringValue(configuration.Name)
	state.Description = optionalStringValue(configuration.Description)
	state.ChannelType = types.StringValue(configuration.ChannelType)
	state.DestinationUrl = types.StringValue(configuration.DestinationURL)
	// signing_secret is kept from the state: Terrakube never returns it (its
	// "read notification signing secret" check always denies), so the response
	// value is always empty.
	state.Active = types.BoolValue(configuration.Active)
	state.MessageStyle = types.StringValue(configuration.MessageStyle)
	state.TriggerStatuses = triggerList
	state.TemplateIds = templateList

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *OrganizationNotificationConfigurationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan OrganizationNotificationConfigurationResourceModel
	var state OrganizationNotificationConfigurationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var desiredStatuses []string
	resp.Diagnostics.Append(plan.TriggerStatuses.ElementsAs(ctx, &desiredStatuses, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var desiredTemplateIds []string
	if !plan.TemplateIds.IsNull() && !plan.TemplateIds.IsUnknown() {
		resp.Diagnostics.Append(plan.TemplateIds.ElementsAs(ctx, &desiredTemplateIds, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	signingSecret, secretDiags := signingSecretForUpdate(ctx, req.Config, plan.SigningSecret, state.SigningSecret, state.SigningSecretWoVersion)
	resp.Diagnostics.Append(secretDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.updateString("name", plan.Name, state.Name)
	attributes.updateString("description", plan.Description, state.Description)
	attributes.updateString("channelType", plan.ChannelType, state.ChannelType)
	attributes.updateString("destinationUrl", plan.DestinationUrl, state.DestinationUrl)
	if signingSecret != nil {
		attributes["signingSecret"] = *signingSecret
	}
	attributes.updateBool("active", plan.Active, state.Active)
	attributes.updateString("messageStyle", plan.MessageStyle, state.MessageStyle)

	id := state.ID.ValueString()
	configuration, err := r.client.NotificationConfigurations.UpdateFieldsByID(ctx, id, terrakube.Patch{
		Attributes:    attributes,
		Relationships: notificationTemplatesRelationship(desiredTemplateIds),
	})
	if patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating organization notification configuration", err)
		return
	}

	configuration, err = readAfterPatch(configuration, func() (*terrakube.NotificationConfiguration, error) {
		return r.client.NotificationConfigurations.GetByID(ctx, id)
	})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading organization notification configuration after update", err)
		return
	}

	current, err := r.client.NotificationTriggers.ListByConfiguration(ctx, id, nil)
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading notification triggers", err)
		return
	}
	resp.Diagnostics.Append(syncNotificationTriggers(ctx, r.client, id, current, desiredStatuses)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = state.ID
	plan.Name = types.StringValue(configuration.Name)
	plan.Description = optionalStringValue(configuration.Description)
	plan.ChannelType = types.StringValue(configuration.ChannelType)
	plan.DestinationUrl = types.StringValue(configuration.DestinationURL)
	// signing_secret stays as planned: Terrakube never returns it (see the Read comment).
	plan.Active = types.BoolValue(configuration.Active)
	plan.MessageStyle = types.StringValue(configuration.MessageStyle)
	triggerList, listDiags := types.ListValueFrom(ctx, types.StringType, desiredStatuses)
	resp.Diagnostics.Append(listDiags...)
	plan.TriggerStatuses = triggerList
	templateList, templateListDiags := types.ListValueFrom(ctx, types.StringType, desiredTemplateIds)
	resp.Diagnostics.Append(templateListDiags...)
	plan.TemplateIds = templateList

	plan.SigningSecretWo = types.StringNull()
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *OrganizationNotificationConfigurationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state OrganizationNotificationConfigurationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.NotificationConfigurations.DeleteByID(ctx, state.ID.ValueString())
	reportDeleteError(&resp.Diagnostics, "deleting organization notification configuration", err)
}

func (r *OrganizationNotificationConfigurationResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return signingSecretConfigValidators()
}

func (r *OrganizationNotificationConfigurationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")
	if len(idParts) != 2 || idParts[0] == "" || idParts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: 'organization_ID,ID', Got: %q", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[1])...)
}
