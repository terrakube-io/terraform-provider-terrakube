package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	terrakube "github.com/terrakube-io/terrakube-go"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &WorkspaceWebhookV2Resource{}
var _ resource.ResourceWithImportState = &WorkspaceWebhookV2Resource{}

type WorkspaceWebhookV2Resource struct {
	client *terrakube.Client
}

type WorkspaceWebhookV2ResourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	WorkspaceId    types.String `tfsdk:"workspace_id"`
	RemoteHookId   types.String `tfsdk:"remote_hook_id"`
	MigratedV2     types.Bool   `tfsdk:"migrated_v2"`
}

func NewWorkspaceWebhookV2Resource() resource.Resource {
	return &WorkspaceWebhookV2Resource{}
}

func (r *WorkspaceWebhookV2Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_webhook_v2"
}

func (r *WorkspaceWebhookV2Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
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
			"remote_hook_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "The remote hook ID.",
			},
			"migrated_v2": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the webhook has been migrated to v2. Enables the webhook v2 processing path.",
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *WorkspaceWebhookV2Resource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Workspace Webhook Resource")
}

func (r *WorkspaceWebhookV2Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceWebhookV2ResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	webhookID := uuid.New().String()
	tflog.Debug(ctx, "Creating webhook request", map[string]any{
		"id": webhookID,
	})

	// The webhook is created with an atomic operation on the workspace's webhook collection, as
	// before. The old request also carried a "relationships" member next to "data", outside the
	// resource object, where the server ignores it; terrakube-go's Operation has no such member.
	migratedV2 := plan.MigratedV2.ValueBool()
	result, err := r.client.Operations.Submit(ctx, &terrakube.AtomicRequest{Operations: []terrakube.Operation{{
		Op:   terrakube.OperationAdd,
		Href: fmt.Sprintf("/organization/%s/workspace/%s/webhook", plan.OrganizationId.ValueString(), plan.WorkspaceId.ValueString()),
		Data: map[string]any{
			"type": "webhook",
			"id":   webhookID,
			"attributes": map[string]any{
				"migratedV2": migratedV2,
			},
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
	plan.ID = types.StringValue(id)
	plan.RemoteHookId = types.StringValue(id) // Using ID as RemoteHookId since it's not in the response
	plan.MigratedV2 = types.BoolValue(migratedV2)

	tflog.Debug(ctx, "Successfully created webhook", map[string]any{
		"id":             id,
		"remote_hook_id": id,
	})

	tflog.Info(ctx, "Workspace Webhook Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceWebhookV2Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceWebhookV2ResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	webhook, err := r.client.Webhooks.Get(ctx, state.OrganizationId.ValueString(), state.WorkspaceId.ValueString(), state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace webhook", err)
		return
	}

	state.ID = types.StringValue(webhook.ID)
	state.RemoteHookId = types.StringValue(webhook.RemoteHookID)
	state.MigratedV2 = types.BoolValue(webhook.MigratedV2)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Workspace Webhook Resource reading", map[string]any{"success": true})
}

func (r *WorkspaceWebhookV2Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan WorkspaceWebhookV2ResourceModel
	var state WorkspaceWebhookV2ResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// migratedV2 is always sent, false when the plan has no value, as the old request did.
	organizationID, workspaceID, id := state.OrganizationId.ValueString(), state.WorkspaceId.ValueString(), state.ID.ValueString()
	_, err := r.client.Webhooks.UpdateFields(ctx, organizationID, workspaceID, id, terrakube.Patch{
		Attributes: map[string]any{"migratedV2": plan.MigratedV2.ValueBool()},
	})
	if patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating workspace webhook", err)
		return
	}

	webhook, err := r.client.Webhooks.Get(ctx, organizationID, workspaceID, id)
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace webhook after update", err)
		return
	}

	plan.ID = types.StringValue(id)
	plan.RemoteHookId = types.StringValue(webhook.RemoteHookID)
	plan.MigratedV2 = types.BoolValue(webhook.MigratedV2)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceWebhookV2Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data WorkspaceWebhookV2ResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Webhooks.Delete(ctx, data.OrganizationId.ValueString(), data.WorkspaceId.ValueString(), data.ID.ValueString())
	if err != nil && !terrakube.IsNotFound(err) {
		reportClientError(&resp.Diagnostics, "deleting workspace webhook", err)
		return
	}
}

func (r *WorkspaceWebhookV2Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
