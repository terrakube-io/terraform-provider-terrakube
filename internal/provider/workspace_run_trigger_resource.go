package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	terrakube "github.com/terrakube-io/terrakube-go"
)

var _ resource.Resource = &WorkspaceRunTriggerResource{}
var _ resource.ResourceWithImportState = &WorkspaceRunTriggerResource{}

type WorkspaceRunTriggerResource struct {
	client *terrakube.Client
}

type WorkspaceRunTriggerResourceModel struct {
	ID                     types.String `tfsdk:"id"`
	SourceWorkspaceID      types.String `tfsdk:"source_workspace_id"`
	DestinationWorkspaceID types.String `tfsdk:"destination_workspace_id"`
	TemplateID             types.String `tfsdk:"template_id"`
	Enabled                types.Bool   `tfsdk:"enabled"`
}

func NewWorkspaceRunTriggerResource() resource.Resource { return &WorkspaceRunTriggerResource{} }

func (r *WorkspaceRunTriggerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_run_trigger"
}

func (r *WorkspaceRunTriggerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Creates a directed run trigger: a successful apply in the source workspace queues a run in the destination workspace. Requires Terrakube 2.34.0 or later.",
		Attributes: map[string]schema.Attribute{
			"id":                       schema.StringAttribute{Computed: true, Description: "Run trigger ID (UUID)", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"source_workspace_id":      schema.StringAttribute{Required: true, Description: "ID of the upstream workspace whose successful apply fires the trigger. Changing this forces a new resource.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"destination_workspace_id": schema.StringAttribute{Required: true, Description: "ID of the downstream workspace that receives the triggered run. Changing this forces a new resource.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"template_id":              schema.StringAttribute{Optional: true, Description: "Optional template ID for the triggered run. When unset, the destination workspace default template is used."},
			"enabled":                  schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true), Description: "Whether this trigger is active."},
		},
	}
}

func (r *WorkspaceRunTriggerResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Workspace Run Trigger Resource")
}

func (r *WorkspaceRunTriggerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceRunTriggerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sourceID, destinationID := plan.SourceWorkspaceID.ValueString(), plan.DestinationWorkspaceID.ValueString()
	patch := terrakube.Patch{
		Attributes: patchAttributes{"enabled": plan.Enabled.ValueBool()},
		Relationships: map[string]terrakube.Relationship{
			"sourceWorkspace":      {Type: "workspace", ID: &sourceID},
			"destinationWorkspace": {Type: "workspace", ID: &destinationID},
			"template":             workspaceRunTriggerTemplate(plan),
		},
	}
	trigger, err := r.client.RunTriggers.CreateFieldsTopLevel(ctx, patch)
	if terrakube.IsNotFound(err) {
		resp.Diagnostics.AddError("Error creating workspace run trigger", "Terrakube did not create a run trigger. Confirm that the endpoint is available (run triggers require Terrakube 2.34.0 or later).")
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating workspace run trigger", err)
		return
	}
	setWorkspaceRunTriggerState(&plan, trigger)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceRunTriggerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceRunTriggerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	trigger, err := r.client.RunTriggers.GetByID(ctx, state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace run trigger", err)
		return
	}
	setWorkspaceRunTriggerState(&state, trigger)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *WorkspaceRunTriggerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var state, plan WorkspaceRunTriggerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// sourceWorkspace and destinationWorkspace are left out. They are ForceNew in Terraform and
	// superuser-only on the API, so resending them during an enabled/template PATCH is
	// unnecessary and can cause an authorization failure on stricter servers. The template is
	// always sent, as null when template_id is unset, so removing it clears the link.
	patch := terrakube.Patch{
		Attributes:    patchAttributes{"enabled": plan.Enabled.ValueBool()},
		Relationships: map[string]terrakube.Relationship{"template": workspaceRunTriggerTemplate(plan)},
	}
	id := state.ID.ValueString()
	trigger, err := r.client.RunTriggers.UpdateFieldsByID(ctx, id, patch)
	if terrakube.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating workspace run trigger", err)
		return
	}
	// Read after a successful update so state reflects any server-side normalization.
	trigger, err = readAfterPatch(trigger, func() (*terrakube.RunTrigger, error) {
		return r.client.RunTriggers.GetByID(ctx, id)
	})
	if terrakube.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading updated workspace run trigger", err)
		return
	}
	setWorkspaceRunTriggerState(&plan, trigger)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceRunTriggerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state WorkspaceRunTriggerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.RunTriggers.DeleteByID(ctx, state.ID.ValueString())
	reportDeleteError(&resp.Diagnostics, "deleting workspace run trigger", err)
}

func (r *WorkspaceRunTriggerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// workspaceRunTriggerTemplate links the trigger to its template, or clears the link
// ({"data": null}) when template_id is unset or empty.
func workspaceRunTriggerTemplate(model WorkspaceRunTriggerResourceModel) terrakube.Relationship {
	if model.TemplateID.IsNull() || model.TemplateID.IsUnknown() || model.TemplateID.ValueString() == "" {
		return terrakube.Relationship{}
	}
	templateID := model.TemplateID.ValueString()
	return terrakube.Relationship{Type: "template", ID: &templateID}
}

func setWorkspaceRunTriggerState(model *WorkspaceRunTriggerResourceModel, trigger *terrakube.RunTrigger) {
	model.ID = types.StringValue(trigger.ID)
	model.Enabled = types.BoolValue(trigger.Enabled)
	if trigger.SourceWorkspace != nil {
		model.SourceWorkspaceID = types.StringValue(trigger.SourceWorkspace.ID)
	}
	if trigger.DestinationWorkspace != nil {
		model.DestinationWorkspaceID = types.StringValue(trigger.DestinationWorkspace.ID)
	}
	if trigger.Template == nil {
		model.TemplateID = types.StringNull()
	} else {
		model.TemplateID = types.StringValue(trigger.Template.ID)
	}
}
