package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	terrakube "github.com/terrakube-io/terrakube-go"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &WorkspaceTagResource{}
var _ resource.ResourceWithImportState = &WorkspaceTagResource{}

type WorkspaceTagResource struct {
	client *terrakube.Client
}

type WorkspaceTagResourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	WorkspaceId    types.String `tfsdk:"workspace_id"`
	TagID          types.String `tfsdk:"tag_id"`
	Value          types.String `tfsdk:"value"`
}

func NewWorkspaceTagResource() resource.Resource {
	return &WorkspaceTagResource{}
}

func (r *WorkspaceTagResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_tag"
}

func (r *WorkspaceTagResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Adds a tag to a workspace resource. Set `value` to make it a key/value tag, which the Terraform CLI `cloud` block can select with `tags = { key = \"value\" }`.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Workspace Tag Id",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"tag_id": schema.StringAttribute{
				Required:    true,
				Description: "Tag Id. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"value": schema.StringAttribute{
				Optional:    true,
				Description: "Value of the tag in this workspace (Terrakube 2.34.0+). Omit it for a key-only tag, which works on older versions too.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 256),
				},
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization id. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube workspace id. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *WorkspaceTagResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Workspace Tag Resource")
}

func (r *WorkspaceTagResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceTagResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// value is left out for a key-only tag, so key-only tags can still be created on Terrakube
	// versions without key/value tags, whose API rejects the unknown attribute.
	attributes := patchAttributes{}
	attributes.createString("tagId", plan.TagID)
	attributes.createString("value", plan.Value)

	workspaceTag, err := r.client.WorkspaceTags.CreateFields(ctx, plan.OrganizationId.ValueString(), plan.WorkspaceId.ValueString(), terrakube.Patch{Attributes: attributes})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating workspace tag", err)
		return
	}

	plan.ID = types.StringValue(workspaceTag.ID)
	plan.TagID = types.StringValue(workspaceTag.TagID)
	plan.Value = workspaceTagValue(workspaceTag.Value)

	tflog.Info(ctx, "Workspace Tag Resource Created", map[string]any{"success": true})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceTagResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan WorkspaceTagResourceModel
	var state WorkspaceTagResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Only the value can change in place; every other attribute requires replacement. The value
	// is always sent, so removing it from the configuration turns the tag back into a key-only
	// tag (null) instead of leaving the previous value in place.
	attributes := patchAttributes{"value": plan.Value.ValueStringPointer()}

	organizationID, workspaceID, id := state.OrganizationId.ValueString(), state.WorkspaceId.ValueString(), state.ID.ValueString()
	_, err := r.client.WorkspaceTags.UpdateFields(ctx, organizationID, workspaceID, id, terrakube.Patch{Attributes: attributes})
	if patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating workspace tag", err)
		return
	}

	workspaceTag, err := r.client.WorkspaceTags.Get(ctx, organizationID, workspaceID, id)
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace tag after update", err)
		return
	}

	plan.ID = state.ID
	plan.TagID = types.StringValue(workspaceTag.TagID)
	plan.Value = workspaceTagValue(workspaceTag.Value)

	tflog.Info(ctx, "Workspace Tag Resource Updated", map[string]any{"success": true})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// workspaceTagValue maps the API value to state. The API stores a key-only tag as null, and the TFE API
// reports it as "", so both become null here.
func workspaceTagValue(value *string) types.String {
	if value == nil || *value == "" {
		return types.StringNull()
	}
	return types.StringValue(*value)
}

func (r *WorkspaceTagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceTagResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceTag, err := r.client.WorkspaceTags.Get(ctx, state.OrganizationId.ValueString(), state.WorkspaceId.ValueString(), state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Workspace tag not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace tag", err)
		return
	}

	state.ID = types.StringValue(workspaceTag.ID)
	state.TagID = types.StringValue(workspaceTag.TagID)
	state.Value = workspaceTagValue(workspaceTag.Value)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *WorkspaceTagResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data WorkspaceTagResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// 404 means the tag is already gone, which is what destroy wants.
	err := r.client.WorkspaceTags.Delete(ctx, data.OrganizationId.ValueString(), data.WorkspaceId.ValueString(), data.ID.ValueString())
	reportDeleteError(&resp.Diagnostics, "deleting workspace tag", err)
}

func (r *WorkspaceTagResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.AddError("Import not implemented", "Import is not implemented for Workspace Tag Resource, please delete and recreate the resource")
}
