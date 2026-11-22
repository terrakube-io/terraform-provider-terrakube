package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
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
var _ resource.Resource = &WorkspaceScheduleResource{}
var _ resource.ResourceWithImportState = &WorkspaceScheduleResource{}

type WorkspaceScheduleResource struct {
	client *terrakube.Client
}

type WorkspaceScheduleResourceModel struct {
	ID          types.String `tfsdk:"id"`
	WorkspaceId types.String `tfsdk:"workspace_id"`
	TemplateId  types.String `tfsdk:"template_id"`
	Schedule    types.String `tfsdk:"schedule"`
}

func NewWorkspaceScheduleResource() resource.Resource {
	return &WorkspaceScheduleResource{}
}

func (r *WorkspaceScheduleResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_schedule"
}

func (r *WorkspaceScheduleResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a workspace schedule that will allow you to run templates on a regular basis.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Schedule Id",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"schedule": schema.StringAttribute{
				Required:    true,
				Description: "Schedule as a Quartz cron expression: seconds minutes hours day-of-month month day-of-week [year], for example `0 0 2 * * ?`. It is not a Unix cron expression, and exactly one of day-of-month and day-of-week must be `?`.",
				Validators:  []validator.String{quartzCronValidator{}},
			},
			"template_id": schema.StringAttribute{
				Required:    true,
				Description: "Template Id to be used when triggering a job",
			},
			"workspace_id": schema.StringAttribute{
				Required:    true,
				Description: "Workspace Id",
			},
		},
	}
}

func (r *WorkspaceScheduleResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Workspace Schedule Resource")
}

func (r *WorkspaceScheduleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceScheduleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.createString("cron", plan.Schedule)
	attributes.createString("templateReference", plan.TemplateId)

	workspaceSchedule, err := r.client.WorkspaceSchedules.CreateFields(ctx, plan.WorkspaceId.ValueString(), terrakube.Patch{Attributes: attributes})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating workspace schedule", err)
		return
	}

	plan.ID = types.StringValue(workspaceSchedule.ID)
	plan.Schedule = types.StringValue(workspaceSchedule.Schedule)
	plan.TemplateId = types.StringValue(workspaceSchedule.TemplateID)

	tflog.Info(ctx, "workspace schedule Resource Created", map[string]any{"success": true})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceScheduleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceScheduleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceSchedule, err := r.client.WorkspaceSchedules.Get(ctx, state.WorkspaceId.ValueString(), state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Workspace schedule not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace schedule", err)
		return
	}

	state.ID = types.StringValue(workspaceSchedule.ID)
	state.Schedule = types.StringValue(workspaceSchedule.Schedule)
	state.TemplateId = types.StringValue(workspaceSchedule.TemplateID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *WorkspaceScheduleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan WorkspaceScheduleResourceModel
	var state WorkspaceScheduleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.updateString("cron", plan.Schedule, state.Schedule)
	attributes.updateString("templateReference", plan.TemplateId, state.TemplateId)

	workspaceID, id := state.WorkspaceId.ValueString(), state.ID.ValueString()
	_, err := r.client.WorkspaceSchedules.UpdateFields(ctx, workspaceID, id, terrakube.Patch{Attributes: attributes})
	if patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating workspace schedule", err)
		return
	}

	workspaceSchedule, err := r.client.WorkspaceSchedules.Get(ctx, workspaceID, id)
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace schedule after update", err)
		return
	}

	plan.ID = types.StringValue(id)
	plan.Schedule = types.StringValue(workspaceSchedule.Schedule)
	plan.TemplateId = types.StringValue(workspaceSchedule.TemplateID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceScheduleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data WorkspaceScheduleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.WorkspaceSchedules.Delete(ctx, data.WorkspaceId.ValueString(), data.ID.ValueString())
	if err != nil && !terrakube.IsNotFound(err) {
		reportClientError(&resp.Diagnostics, "deleting workspace schedule", err)
	}
}

func (r *WorkspaceScheduleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Read only needs the workspace and the schedule id.
	idParts := strings.Split(req.ID, ",")

	if len(idParts) != 2 || idParts[0] == "" || idParts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: 'workspace_ID,ID', Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[1])...)
}
