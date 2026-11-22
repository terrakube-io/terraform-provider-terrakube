package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	terrakube "github.com/terrakube-io/terrakube-go"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &WorkspaceWebhookEventResource{}
var _ resource.ResourceWithImportState = &WorkspaceWebhookEventResource{}

type WorkspaceWebhookEventResource struct {
	client *terrakube.Client
}

type WorkspaceWebhookEventResourceModel struct {
	ID                types.String `tfsdk:"id"`
	WebhookId         types.String `tfsdk:"webhook_id"`
	Event             types.String `tfsdk:"event"`
	Branch            types.List   `tfsdk:"branch"`
	Path              types.List   `tfsdk:"path"`
	PathType          types.String `tfsdk:"path_type"`
	Priority          types.Int64  `tfsdk:"priority"`
	TemplateId        types.String `tfsdk:"template_id"`
	PrWorkflowEnabled types.Bool   `tfsdk:"pr_workflow_enabled"`
	PrApplyEnabled    types.Bool   `tfsdk:"pr_apply_enabled"`
}

// Allowed values for the path_type attribute. They mirror the server-side
// WebhookEventPathType enum.
const (
	webhookEventPathTypePattern = "PATTERN"
	webhookEventPathTypeRegex   = "REGEX"
)

// webhookEventPathTypeToState converts the pathType attribute returned by the
// API into a Terraform value. Servers older than 2.31.0 do not return the
// attribute at all, and the server itself treats a null pathType as REGEX, so
// an empty value is normalized to REGEX to match the schema default and avoid
// a perpetual diff.
func webhookEventPathTypeToState(pathType string) types.String {
	if pathType == "" {
		return types.StringValue(webhookEventPathTypeRegex)
	}
	return types.StringValue(pathType)
}

func NewWorkspaceWebhookEventResource() resource.Resource {
	return &WorkspaceWebhookEventResource{}
}

func (r *WorkspaceWebhookEventResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_webhook_event"
}

func (r *WorkspaceWebhookEventResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a webhook event attached to a webhook. Defines when and how the webhook should trigger.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Webhook Event ID",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"webhook_id": schema.StringAttribute{
				Required:    true,
				Description: "The ID of the webhook this event belongs to. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"path": schema.ListAttribute{
				Optional:    true,
				Description: "A list of file paths that must match a changed file for a run to trigger. Each entry is interpreted according to `path_type`: a regular expression when `REGEX` (default), or a simple wildcard pattern such as `terraform/*` or `modules/**` when `PATTERN`.",
				ElementType: types.StringType,
			},
			"path_type": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(webhookEventPathTypeRegex),
				Description: "How entries in `path` are matched against changed files. `REGEX` (default) treats each entry as a full regular expression. `PATTERN` treats each entry as a simple wildcard pattern where `*` matches any sequence of characters and `?` matches a single character. Branch matching always uses regex regardless of this setting. Requires Terrakube 2.31.0 or later; older servers ignore this attribute.",
				Validators: []validator.String{
					stringvalidator.OneOf(webhookEventPathTypePattern, webhookEventPathTypeRegex),
				},
			},
			"branch": schema.ListAttribute{
				Optional:    true,
				Description: "A list of branches that trigger a run. Support regex for more complex matching.",
				ElementType: types.StringType,
			},
			"priority": schema.Int64Attribute{
				Optional:    true,
				Description: "The priority of this webhook event",
				Computed:    true,
			},
			"template_id": schema.StringAttribute{
				Optional:    true,
				Description: "The template id to use for the run.",
			},
			"event": schema.StringAttribute{
				Optional:    true,
				Description: "The event type that triggers a run. Supported values: `PUSH`, `PULL_REQUEST`, `RELEASE`.",
				Computed:    true,
				Default:     stringdefault.StaticString("PUSH"),
				Validators: []validator.String{
					stringvalidator.OneOf("PUSH", "PULL_REQUEST", "RELEASE"),
				},
			},
			"pr_workflow_enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Post plan results as a comment on the pull/merge request (`PULL_REQUEST` events only), and accept a `terrakube plan` PR-comment command to re-run it.",
			},
			"pr_apply_enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Allow the `terrakube apply` PR-comment command to apply this workspace (`PULL_REQUEST` events only). Requires `pr_workflow_enabled` to also be true. Requires a Terrakube release including this field (targeted for 2.33.0); older instances will ignore or reject it.",
			},
		},
	}
}

func (r *WorkspaceWebhookEventResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Workspace Webhook Event Resource")
}

// webhookEventAttributes returns the attributes of the webhook event in plan, as the atomic add
// and update operations send them: every attribute, with the branch and path lists joined by
// commas.
func webhookEventAttributes(ctx context.Context, plan WorkspaceWebhookEventResourceModel) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	var branchList, pathList []string
	if !plan.Branch.IsNull() && !plan.Branch.IsUnknown() {
		diags.Append(plan.Branch.ElementsAs(ctx, &branchList, false)...)
	}
	if !plan.Path.IsNull() && !plan.Path.IsUnknown() {
		diags.Append(plan.Path.ElementsAs(ctx, &pathList, false)...)
	}
	return map[string]any{
		"priority":          plan.Priority.ValueInt64(),
		"event":             plan.Event.ValueString(),
		"branch":            strings.Join(branchList, ","),
		"path":              strings.Join(pathList, ","),
		"pathType":          plan.PathType.ValueString(),
		"templateId":        plan.TemplateId.ValueString(),
		"prWorkflowEnabled": plan.PrWorkflowEnabled.ValueBool(),
		"prApplyEnabled":    plan.PrApplyEnabled.ValueBool(),
	}, diags
}

func (r *WorkspaceWebhookEventResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceWebhookEventResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// First, check that the webhook exists
	_, err := r.client.Webhooks.GetByID(ctx, plan.WebhookId.ValueString())
	if terrakube.IsNotFound(err) {
		resp.Diagnostics.AddError("Webhook not found", fmt.Sprintf("Webhook with ID %s not found", plan.WebhookId.ValueString()))
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace webhook", err)
		return
	}

	eventID := uuid.New().String()
	tflog.Debug(ctx, "Creating webhook event request", map[string]any{
		"id": eventID,
	})

	attributes, diags := webhookEventAttributes(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := r.client.Operations.Submit(ctx, &terrakube.AtomicRequest{Operations: []terrakube.Operation{{
		Op:   terrakube.OperationAdd,
		Href: fmt.Sprintf("/webhook/%s/events", plan.WebhookId.ValueString()),
		Data: map[string]any{
			"type":       "webhook_event",
			"id":         eventID,
			"attributes": attributes,
		},
	}}})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating or updating workspace webhook event", err)
		return
	}

	if len(result.Results) == 0 {
		resp.Diagnostics.AddError(
			"Invalid webhook event response",
			"No results returned from atomic operation",
		)
		return
	}

	id, _ := result.Results[0].Data["id"].(string)
	plan.ID = types.StringValue(id)
	plan.Priority = types.Int64Value(plan.Priority.ValueInt64()) // the priority sent: 0 when the plan had none

	tflog.Info(ctx, "Workspace Webhook Event Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceWebhookEventResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data WorkspaceWebhookEventResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// First, get the webhook to see whether it still has the event
	webhook, err := r.client.Webhooks.GetByID(ctx, data.WebhookId.ValueString())
	if terrakube.IsNotFound(err) {
		// If the webhook is not found, we can consider the event deleted
		tflog.Info(ctx, "Webhook not found, considering event deleted", map[string]any{"success": true})
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace webhook", err)
		return
	}

	// Check if the event still exists in the webhook's events relationship
	eventFound := false
	for _, event := range webhook.Events {
		if event != nil && event.ID == data.ID.ValueString() {
			eventFound = true
			break
		}
	}

	if !eventFound {
		// If the event is not found in the webhook's events, we can consider it deleted
		tflog.Info(ctx, "Event not found in webhook's events, considering it deleted", map[string]any{"success": true})
		return
	}

	// Use atomic operation to delete the event
	_, err = r.client.Operations.Submit(ctx, &terrakube.AtomicRequest{Operations: []terrakube.Operation{{
		Op:   terrakube.OperationRemove,
		Href: fmt.Sprintf("/webhook/%s/events/%s", data.WebhookId.ValueString(), data.ID.ValueString()),
	}}})
	if err != nil && !terrakube.IsNotFound(err) {
		reportClientError(&resp.Diagnostics, "deleting workspace webhook event", err)
		return
	}

	tflog.Info(ctx, "Workspace Webhook Event Resource Deleted", map[string]any{"success": true})
}

// webhookEventToModel copies the attributes of event into model. Branch and path are split on
// commas, and a missing pathType is REGEX.
func webhookEventToModel(ctx context.Context, event *terrakube.WebhookEvent, model *WorkspaceWebhookEventResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	model.ID = types.StringValue(event.ID)
	model.Event = types.StringValue(event.Event)
	model.TemplateId = types.StringValue(event.TemplateID)
	model.Priority = types.Int64Value(int64(event.Priority))
	model.PrWorkflowEnabled = types.BoolValue(event.PRWorkflowEnabled)
	model.PrApplyEnabled = types.BoolValue(event.PRApplyEnabled)

	branchList, branchDiags := types.ListValueFrom(ctx, types.StringType, strings.Split(event.Branch, ","))
	diags.Append(branchDiags...)
	model.Branch = branchList

	pathList, pathDiags := types.ListValueFrom(ctx, types.StringType, strings.Split(event.Path, ","))
	diags.Append(pathDiags...)
	model.Path = pathList
	model.PathType = webhookEventPathTypeToState(event.PathType)
	return diags
}

func (r *WorkspaceWebhookEventResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceWebhookEventResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	event, err := r.client.WebhookEvents.GetByID(ctx, state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace webhook event", err)
		return
	}

	webhookID := ""
	if event.Webhook != nil {
		webhookID = event.Webhook.ID
	}
	state.WebhookId = types.StringValue(webhookID)
	resp.Diagnostics.Append(webhookEventToModel(ctx, event, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Workspace Webhook Event Resource reading", map[string]any{"success": true})
}

func (r *WorkspaceWebhookEventResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan WorkspaceWebhookEventResourceModel
	var state WorkspaceWebhookEventResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Look up the webhook via plan.WebhookId, not state.WebhookId: when the
	// parent terrakube_workspace_webhook_v2 is replaced (taint, or any other
	// forced recreation), webhook_id changes and state still holds the old,
	// now-deleted webhook's ID. Looking it up by the old ID here would 404
	// even though the update itself is perfectly valid against the new one.
	webhook, err := r.client.Webhooks.GetByID(ctx, plan.WebhookId.ValueString())
	if terrakube.IsNotFound(err) {
		resp.Diagnostics.AddError("Webhook not found", fmt.Sprintf("Webhook with ID %s not found", plan.WebhookId.ValueString()))
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace webhook", err)
		return
	}

	workspaceId := ""
	if webhook.Workspace != nil {
		workspaceId = webhook.Workspace.ID
	}
	tflog.Debug(ctx, "Parsed webhook details", map[string]any{
		"workspace_id": workspaceId,
		"webhook_id":   webhook.ID,
	})
	if workspaceId == "" {
		resp.Diagnostics.AddError("Invalid webhook response", "Could not determine Workspace ID from webhook response")
		return
	}

	// Get organization ID from workspace
	workspace, err := r.client.Workspaces.GetByID(ctx, workspaceId)
	if terrakube.IsNotFound(err) {
		resp.Diagnostics.AddError("Workspace not found", fmt.Sprintf("Workspace with ID %s not found", workspaceId))
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace", err)
		return
	}

	organizationId := ""
	if workspace.Organization != nil {
		organizationId = workspace.Organization.ID
	}
	if organizationId == "" {
		resp.Diagnostics.AddError("Invalid workspace response", "Could not determine Organization ID from workspace response")
		return
	}

	tflog.Debug(ctx, "Parsed workspace details", map[string]any{
		"organization_id": organizationId,
		"workspace_id":    workspaceId,
	})

	attributes, diags := webhookEventAttributes(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := r.client.Operations.Submit(ctx, &terrakube.AtomicRequest{Operations: []terrakube.Operation{{
		Op: terrakube.OperationUpdate,
		Href: fmt.Sprintf("/organization/%s/workspace/%s/webhook/%s/events/%s",
			organizationId,
			workspaceId,
			plan.WebhookId.ValueString(),
			state.ID.ValueString()),
		Data: map[string]any{
			"type":       "webhook_event",
			"id":         state.ID.ValueString(),
			"attributes": attributes,
		},
	}}})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating or updating workspace webhook event", err)
		return
	}

	if len(result.Results) == 0 {
		resp.Diagnostics.AddError(
			"Invalid webhook event response",
			"No results returned from atomic operation",
		)
		return
	}

	// Read the updated webhook event to get all attributes
	events, err := r.client.WebhookEvents.List(ctx, organizationId, workspaceId, plan.WebhookId.ValueString(), nil)
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace webhook event after update", err)
		return
	}

	// Find the event with matching ID
	var foundEvent *terrakube.WebhookEvent
	for _, event := range events {
		if event != nil && event.ID == state.ID.ValueString() {
			foundEvent = event
			break
		}
	}

	if foundEvent == nil {
		resp.Diagnostics.AddError("Error updating webhook event", "Could not find updated event in response")
		return
	}

	resp.Diagnostics.Append(webhookEventToModel(ctx, foundEvent, &plan)...)
	plan.ID = types.StringValue(state.ID.ValueString())
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceWebhookEventResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) != 2 || idParts[0] == "" || idParts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: 'webhook_ID,event_ID', Got: %q", req.ID),
		)
		return
	}

	// Use path.Root directly to make the import usage more explicit
	webhookIdPath := path.Root("webhook_id")
	idPath := path.Root("id")

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, webhookIdPath, idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, idPath, idParts[1])...)
}
