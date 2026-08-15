package provider

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"terraform-provider-terrakube/internal/client"

	"github.com/google/jsonapi"
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
)

var _ resource.Resource = &WorkspaceNotificationConfigurationResource{}
var _ resource.ResourceWithImportState = &WorkspaceNotificationConfigurationResource{}

type WorkspaceNotificationConfigurationResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type WorkspaceNotificationConfigurationResourceModel struct {
	ID              types.String `tfsdk:"id"`
	OrganizationId  types.String `tfsdk:"organization_id"`
	WorkspaceId     types.String `tfsdk:"workspace_id"`
	Name            types.String `tfsdk:"name"`
	Description     types.String `tfsdk:"description"`
	ChannelType     types.String `tfsdk:"channel_type"`
	DestinationUrl  types.String `tfsdk:"destination_url"`
	SigningSecret   types.String `tfsdk:"signing_secret"`
	Active          types.Bool   `tfsdk:"active"`
	MessageStyle    types.String `tfsdk:"message_style"`
	TriggerStatuses types.List   `tfsdk:"trigger_statuses"`
	TemplateIds     types.List   `tfsdk:"template_ids"`
}

func NewWorkspaceNotificationConfigurationResource() resource.Resource {
	return &WorkspaceNotificationConfigurationResource{}
}

func (r *WorkspaceNotificationConfigurationResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_notification_configuration"
}

func (r *WorkspaceNotificationConfigurationResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a workspace-level notification configuration (Slack, Teams, or a generic webhook), for the selected job status triggers. This is purely additive alongside any `terrakube_organization_notification_configuration` in the same organization - there is no override or suppression between the two scopes; this workspace still also receives any organization-wide configuration.",

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
			"workspace_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube workspace id",
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
				Description: "If set, outgoing WEBHOOK requests are signed with an X-Terrakube-Signature header (HMAC-SHA256) so the destination can verify they came from Terrakube.",
			},
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

func (r *WorkspaceNotificationConfigurationResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Workspace Notification Configuration Resource Configure Type",
			fmt.Sprintf("Expected *TerrakubeConnectionData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	if providerData.InsecureHttpClient {
		if custom, ok := http.DefaultTransport.(*http.Transport); ok {
			customTransport := custom.Clone()
			customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
			r.client = &http.Client{Transport: customTransport}
		} else {
			r.client = &http.Client{}
		}
	} else {
		r.client = &http.Client{}
	}

	r.endpoint = providerData.Endpoint
	r.token = providerData.Token

	tflog.Debug(ctx, "Configuring Workspace Notification Configuration resource", map[string]any{"success": true})
}

func (r *WorkspaceNotificationConfigurationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceNotificationConfigurationResourceModel
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

	bodyRequest := &client.NotificationConfigurationEntity{
		Name:           plan.Name.ValueString(),
		Description:    plan.Description.ValueStringPointer(),
		ChannelType:    plan.ChannelType.ValueString(),
		DestinationUrl: plan.DestinationUrl.ValueString(),
		SigningSecret:  plan.SigningSecret.ValueStringPointer(),
		Active:         plan.Active.ValueBool(),
		MessageStyle:   plan.MessageStyle.ValueString(),
	}

	out := new(bytes.Buffer)
	if err := jsonapi.MarshalPayload(out, bodyRequest); err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	createRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/notificationConfiguration", r.endpoint, plan.OrganizationId.ValueString(), plan.WorkspaceId.ValueString()), strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace notification configuration resource request", fmt.Sprintf("Error creating workspace notification configuration resource request: %s", err))
		return
	}
	createRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	createRequest.Header.Add("Content-Type", "application/vnd.api+json")

	createResponse, err := r.client.Do(createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace notification configuration resource request", fmt.Sprintf("Error executing workspace notification configuration resource request: %s", err))
		return
	}
	createBody, err := io.ReadAll(createResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading workspace notification configuration resource response")
	}
	if createResponse.StatusCode < 200 || createResponse.StatusCode >= 300 {
		resp.Diagnostics.AddError(
			fmt.Sprintf("Failed to create notification configuration: %s", createResponse.Status),
			string(createBody),
		)
		return
	}

	created := &client.NotificationConfigurationEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(string(createBody)), created); err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	api := notificationConfigAPI{client: r.client, endpoint: r.endpoint, token: r.token}
	resp.Diagnostics.Append(api.syncNotificationTriggers(ctx, created.ID, nil, triggerStatuses)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(api.replaceNotificationConfigurationTemplates(ctx, created.ID, templateIds)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = types.StringValue(created.ID)
	plan.Name = types.StringValue(created.Name)
	plan.Description = optionalStringValue(created.Description)
	plan.ChannelType = types.StringValue(created.ChannelType)
	plan.DestinationUrl = types.StringValue(created.DestinationUrl)
	plan.SigningSecret = optionalStringValue(created.SigningSecret)
	plan.Active = types.BoolValue(created.Active)
	plan.MessageStyle = types.StringValue(created.MessageStyle)
	triggerList, listDiags := types.ListValueFrom(ctx, types.StringType, triggerStatuses)
	resp.Diagnostics.Append(listDiags...)
	plan.TriggerStatuses = triggerList
	templateList, templateListDiags := types.ListValueFrom(ctx, types.StringType, templateIds)
	resp.Diagnostics.Append(templateListDiags...)
	plan.TemplateIds = templateList

	tflog.Info(ctx, "Workspace Notification Configuration Resource Created", map[string]any{"success": true})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceNotificationConfigurationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceNotificationConfigurationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	request, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/notification_configuration/%s", r.endpoint, state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace notification configuration resource request", fmt.Sprintf("Error creating workspace notification configuration resource request: %s", err))
		return
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")

	response, err := r.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace notification configuration resource request", fmt.Sprintf("Error executing workspace notification configuration resource request: %s", err))
		return
	}

	if response.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Notification configuration not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading workspace notification configuration resource response", fmt.Sprintf("Error reading workspace notification configuration resource response: %s", err))
		return
	}

	configuration := &client.NotificationConfigurationEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(string(body)), configuration); err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	api := notificationConfigAPI{client: r.client, endpoint: r.endpoint, token: r.token}
	triggers, diags := api.fetchNotificationTriggers(ctx, state.ID.ValueString())
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

	templateIds, templateDiags := api.fetchNotificationConfigurationTemplateIDs(ctx, state.ID.ValueString())
	resp.Diagnostics.Append(templateDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	templateList, templateListDiags := types.ListValueFrom(ctx, types.StringType, templateIds)
	resp.Diagnostics.Append(templateListDiags...)

	state.ID = types.StringValue(configuration.ID)
	state.Name = types.StringValue(configuration.Name)
	state.Description = optionalStringValue(configuration.Description)
	state.ChannelType = types.StringValue(configuration.ChannelType)
	state.DestinationUrl = types.StringValue(configuration.DestinationUrl)
	state.SigningSecret = optionalStringValue(configuration.SigningSecret)
	state.Active = types.BoolValue(configuration.Active)
	state.MessageStyle = types.StringValue(configuration.MessageStyle)
	state.TriggerStatuses = triggerList
	state.TemplateIds = templateList

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *WorkspaceNotificationConfigurationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan WorkspaceNotificationConfigurationResourceModel
	var state WorkspaceNotificationConfigurationResourceModel
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

	bodyRequest := &client.NotificationConfigurationEntity{
		ID:             state.ID.ValueString(),
		Name:           plan.Name.ValueString(),
		Description:    plan.Description.ValueStringPointer(),
		ChannelType:    plan.ChannelType.ValueString(),
		DestinationUrl: plan.DestinationUrl.ValueString(),
		SigningSecret:  plan.SigningSecret.ValueStringPointer(),
		Active:         plan.Active.ValueBool(),
		MessageStyle:   plan.MessageStyle.ValueString(),
	}

	out := new(bytes.Buffer)
	if err := jsonapi.MarshalPayload(out, bodyRequest); err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	updateRequest, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/notification_configuration/%s", r.endpoint, state.ID.ValueString()), strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace notification configuration resource request", fmt.Sprintf("Error creating workspace notification configuration resource request: %s", err))
		return
	}
	updateRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	updateRequest.Header.Add("Content-Type", "application/vnd.api+json")

	updateResponse, err := r.client.Do(updateRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace notification configuration resource request", fmt.Sprintf("Error executing workspace notification configuration resource request: %s", err))
		return
	}
	updateBody, err := io.ReadAll(updateResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading workspace notification configuration resource response")
	}
	if updateResponse.StatusCode < 200 || updateResponse.StatusCode >= 300 {
		resp.Diagnostics.AddError(
			fmt.Sprintf("Failed to update notification configuration: %s", updateResponse.Status),
			string(updateBody),
		)
		return
	}

	api := notificationConfigAPI{client: r.client, endpoint: r.endpoint, token: r.token}
	current, diags := api.fetchNotificationTriggers(ctx, state.ID.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(api.syncNotificationTriggers(ctx, state.ID.ValueString(), current, desiredStatuses)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(api.replaceNotificationConfigurationTemplates(ctx, state.ID.ValueString(), desiredTemplateIds)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = state.ID
	triggerList, listDiags := types.ListValueFrom(ctx, types.StringType, desiredStatuses)
	resp.Diagnostics.Append(listDiags...)
	plan.TriggerStatuses = triggerList
	templateList, templateListDiags := types.ListValueFrom(ctx, types.StringType, desiredTemplateIds)
	resp.Diagnostics.Append(templateListDiags...)
	plan.TemplateIds = templateList

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceNotificationConfigurationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state WorkspaceNotificationConfigurationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	request, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/notification_configuration/%s", r.endpoint, state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace notification configuration resource request", fmt.Sprintf("Error creating workspace notification configuration resource request: %s", err))
		return
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))

	_, err = r.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace notification configuration resource request", fmt.Sprintf("Error executing workspace notification configuration resource request: %s", err))
		return
	}
}

func (r *WorkspaceNotificationConfigurationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")
	if len(idParts) != 3 || idParts[0] == "" || idParts[1] == "" || idParts[2] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: 'organization_ID,workspace_ID,ID', Got: %q", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), idParts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[2])...)
}
