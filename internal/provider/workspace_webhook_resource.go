package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	terrakube "github.com/terrakube-io/terrakube-go"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &WorkspaceWebhookResource{}
var _ resource.ResourceWithImportState = &WorkspaceWebhookResource{}

// WorkspaceWebhookResource is the v1 webhook. It is not registered in the provider; whether to
// register or delete it is #227. It sends path, branch, templateId and event as attributes of the
// webhook, but current Terrakube keeps them on webhook_event, so registering it as it is would
// likely fail with 400. Read refreshes only id and remote_hook_id, the attributes terrakube-go's
// Webhook has; the others keep their planned value, and an import leaves them null.
type WorkspaceWebhookResource struct {
	client *terrakube.Client
}

type WorkspaceWebhookResourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	WorkspaceId    types.String `tfsdk:"workspace_id"`
	Path           types.List   `tfsdk:"path"`
	Branch         types.List   `tfsdk:"branch"`
	TemplateId     types.String `tfsdk:"template_id"`
	RemoteHookId   types.String `tfsdk:"remote_hook_id"`
	Event          types.String `tfsdk:"event"`
}

func NewWorkspaceWebhookResource() resource.Resource {
	return &WorkspaceWebhookResource{}
}

func (r *WorkspaceWebhookResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_webhook"
}

func (r *WorkspaceWebhookResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a webhook attached to a workspace. Can be useful for automated apply/plan workflows.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Webhook ID",
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
			"path": schema.ListAttribute{
				Optional:    true,
				Description: "The file paths in regex that trigger a run.",
				ElementType: types.StringType,
			},
			"branch": schema.ListAttribute{
				Optional:    true,
				Description: "A list of branches that trigger a run. Support regex for more complex matching.",
				ElementType: types.StringType,
			},
			"template_id": schema.StringAttribute{
				Optional:    true,
				Description: "The template id to use for the run.",
			},
			"remote_hook_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "The remote hook ID.",
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
		},
	}
}

func (r *WorkspaceWebhookResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Workspace Webhook Resource")
}

// webhookV1Attributes is the attribute set of the v1 webhook request, with path and branch
// comma-joined.
func webhookV1Attributes(ctx context.Context, plan WorkspaceWebhookResourceModel, remoteHookID string) map[string]any {
	var branchList, pathList []string
	plan.Branch.ElementsAs(ctx, &branchList, true)
	plan.Path.ElementsAs(ctx, &pathList, true)
	return map[string]any{
		"path":         strings.Join(pathList, ","),
		"branch":       strings.Join(branchList, ","),
		"templateId":   plan.TemplateId.ValueString(),
		"remoteHookId": remoteHookID,
		"event":        plan.Event.ValueString(),
	}
}

func (r *WorkspaceWebhookResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceWebhookResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// The webhook is created with a client-generated ID through an atomic add on the workspace's
	// webhook collection, the way the v2 webhook is created.
	result, err := r.client.Operations.Submit(ctx, &terrakube.AtomicRequest{Operations: []terrakube.Operation{{
		Op:   terrakube.OperationAdd,
		Href: fmt.Sprintf("/organization/%s/workspace/%s/webhook", plan.OrganizationId.ValueString(), plan.WorkspaceId.ValueString()),
		Data: map[string]any{
			"type":       "webhook",
			"id":         uuid.New().String(),
			"attributes": webhookV1Attributes(ctx, plan, ""),
		},
	}}})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating workspace webhook", err)
		return
	}

	if len(result.Results) == 0 {
		resp.Diagnostics.AddError(
			"Invalid webhook response",
			"No results returned from atomic operation",
		)
		return
	}

	id, _ := result.Results[0].Data["id"].(string)
	attributes, _ := result.Results[0].Data["attributes"].(map[string]any)
	remoteHookID, _ := attributes["remoteHookId"].(string)
	plan.ID = types.StringValue(id)
	plan.RemoteHookId = types.StringValue(remoteHookID)

	tflog.Info(ctx, "Workspace Webhook Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceWebhookResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceWebhookResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	webhook, err := r.client.Webhooks.Get(ctx, state.OrganizationId.ValueString(), state.WorkspaceId.ValueString(), state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Workspace webhook not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace webhook", err)
		return
	}

	state.ID = types.StringValue(webhook.ID)
	state.RemoteHookId = types.StringValue(webhook.RemoteHookID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Workspace Webhook Resource reading", map[string]any{"success": true})
}

func (r *WorkspaceWebhookResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan WorkspaceWebhookResourceModel
	var state WorkspaceWebhookResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organizationID, workspaceID, id := state.OrganizationId.ValueString(), state.WorkspaceId.ValueString(), state.ID.ValueString()
	webhook, err := r.client.Webhooks.UpdateFields(ctx, organizationID, workspaceID, id, terrakube.Patch{
		Attributes: webhookV1Attributes(ctx, plan, state.RemoteHookId.ValueString()),
	})
	if patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating workspace webhook", err)
		return
	}

	webhook, err = readAfterPatch(webhook, func() (*terrakube.Webhook, error) {
		return r.client.Webhooks.Get(ctx, organizationID, workspaceID, id)
	})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace webhook after update", err)
		return
	}

	plan.ID = types.StringValue(id)
	plan.RemoteHookId = types.StringValue(webhook.RemoteHookID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceWebhookResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data WorkspaceWebhookResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Webhooks.Delete(ctx, data.OrganizationId.ValueString(), data.WorkspaceId.ValueString(), data.ID.ValueString())
	reportDeleteError(&resp.Diagnostics, "deleting workspace webhook", err)
}

func (r *WorkspaceWebhookResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) != 3 || idParts[0] == "" || idParts[1] == "" || idParts[2] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: 'organization_ID,workspace_ID, ID', Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), idParts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[2])...)
}
