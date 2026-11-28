package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	terrakube "github.com/terrakube-io/terrakube-go"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &CollectionReferenceResource{}
var _ resource.ResourceWithImportState = &CollectionReferenceResource{}

type CollectionReferenceResource struct {
	client *terrakube.Client
}

type CollectionReferenceResourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	CollectionId   types.String `tfsdk:"collection_id"`
	WorkspaceId    types.String `tfsdk:"workspace_id"`
	Description    types.String `tfsdk:"description"`
}

func NewCollectionReferenceResource() resource.Resource {
	return &CollectionReferenceResource{}
}

func (r *CollectionReferenceResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_collection_reference"
}

func (r *CollectionReferenceResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create collection reference that will be used by this workspace only.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Reference Id",
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
			"collection_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube collection id",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Variable description",
			},
		},
	}
}

func (r *CollectionReferenceResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Collection Reference Resource")
}

func (r *CollectionReferenceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan CollectionReferenceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.createString("description", plan.Description)

	patch := terrakube.Patch{Attributes: attributes, Relationships: collectionReferenceRelationships(plan)}
	collectionReference, err := r.client.CollectionReferences.CreateFields(ctx, plan.OrganizationId.ValueString(), plan.CollectionId.ValueString(), patch)
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating collection reference", err)
		return
	}

	if collectionReference.Collection != nil {
		plan.CollectionId = types.StringValue(collectionReference.Collection.ID)
	} else {
		tflog.Warn(ctx, "Collection reference response missing collection relationship data, keeping planned value")
	}
	if collectionReference.Workspace != nil {
		plan.WorkspaceId = types.StringValue(collectionReference.Workspace.ID)
	} else {
		tflog.Warn(ctx, "Collection reference response missing workspace relationship data, keeping planned value")
	}
	plan.Description = types.StringPointerValue(collectionReference.Description)
	plan.ID = types.StringValue(collectionReference.ID)

	tflog.Info(ctx, "collection reference Resource Created", map[string]any{"success": true})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *CollectionReferenceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state CollectionReferenceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	collectionReference, err := r.client.CollectionReferences.Get(ctx, state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Collection reference not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading collection reference", err)
		return
	}

	if collectionReference.Workspace != nil {
		state.WorkspaceId = types.StringValue(collectionReference.Workspace.ID)
	} else {
		tflog.Warn(ctx, "Collection reference response missing workspace relationship data, keeping last-known state value", map[string]any{"id": state.ID.ValueString()})
	}
	if collectionReference.Collection != nil {
		state.CollectionId = types.StringValue(collectionReference.Collection.ID)
	} else {
		tflog.Warn(ctx, "Collection reference response missing collection relationship data, keeping last-known state value", map[string]any{"id": state.ID.ValueString()})
	}
	state.Description = types.StringPointerValue(collectionReference.Description)
	state.ID = types.StringValue(collectionReference.ID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *CollectionReferenceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan CollectionReferenceResourceModel
	var state CollectionReferenceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.updateString("description", plan.Description, state.Description)

	id := state.ID.ValueString()
	patch := terrakube.Patch{Attributes: attributes, Relationships: collectionReferenceRelationships(plan)}
	if _, err := r.client.CollectionReferences.UpdateFields(ctx, id, patch); patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating collection reference", err)
		return
	}

	collectionReference, err := r.client.CollectionReferences.Get(ctx, id)
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading collection reference after update", err)
		return
	}

	plan.ID = types.StringValue(id)
	plan.Description = types.StringPointerValue(collectionReference.Description)
	if collectionReference.Workspace != nil {
		plan.WorkspaceId = types.StringValue(collectionReference.Workspace.ID)
	} else {
		tflog.Warn(ctx, "Collection reference response missing workspace relationship data, keeping planned value")
	}
	if collectionReference.Collection != nil {
		plan.CollectionId = types.StringValue(collectionReference.Collection.ID)
	} else {
		tflog.Warn(ctx, "Collection reference response missing collection relationship data, keeping planned value")
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *CollectionReferenceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data CollectionReferenceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.CollectionReferences.Delete(ctx, data.ID.ValueString())
	if err != nil && !terrakube.IsNotFound(err) {
		reportClientError(&resp.Diagnostics, "deleting collection reference", err)
	}
}

func (r *CollectionReferenceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) != 3 || idParts[0] == "" || idParts[1] == "" || idParts[2] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: 'organization_ID,collection_ID, ID', Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("collection_id"), idParts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), idParts[2])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[3])...)
}

// collectionReferenceRelationships links the reference to its workspace and its collection.
func collectionReferenceRelationships(model CollectionReferenceResourceModel) map[string]terrakube.Relationship {
	workspaceID, collectionID := model.WorkspaceId.ValueString(), model.CollectionId.ValueString()
	return map[string]terrakube.Relationship{
		"workspace":  {Type: "workspace", ID: &workspaceID},
		"collection": {Type: "collection", ID: &collectionID},
	}
}
