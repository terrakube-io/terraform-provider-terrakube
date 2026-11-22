package provider

import (
	"context"
	"fmt"

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
var _ resource.Resource = &OrganizationResource{}
var _ resource.ResourceWithImportState = &OrganizationResource{}

type OrganizationResource struct {
	client *terrakube.Client
}

type OrganizationResourceModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Description   types.String `tfsdk:"description"`
	ExecutionMode types.String `tfsdk:"execution_mode"`
	Icon          types.String `tfsdk:"icon"`
}

func NewOrganizationResource() resource.Resource {
	return &OrganizationResource{}
}

func (r *OrganizationResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (r *OrganizationResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Creates an Organization on Terrakube instance.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Organization Id",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Organization name",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Organization description",
			},
			"execution_mode": schema.StringAttribute{
				Required:    true,
				Description: "Select default execution mode for the organization (remote or local)",
			},
			"icon": schema.StringAttribute{
				Optional:    true,
				Description: "Organization icon in format name:color",
			},
		},
	}
}

func (r *OrganizationResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Organization Resource")
}

func (r *OrganizationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan OrganizationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.createString("name", plan.Name)
	attributes.createString("description", plan.Description)
	attributes.createString("executionMode", plan.ExecutionMode)
	attributes.createString("icon", plan.Icon)

	organization, err := r.client.Organizations.CreateFields(ctx, terrakube.Patch{Attributes: attributes})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating organization", err)
		return
	}

	plan.ID = types.StringValue(organization.ID)
	setOrganizationState(&plan, organization)

	tflog.Info(ctx, "Organization Resource Created", map[string]any{"success": true})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *OrganizationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state OrganizationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organization, err := r.client.Organizations.Get(ctx, state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Organization not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading organization", err)
		return
	}

	state.ID = types.StringValue(organization.ID)
	setOrganizationState(&state, organization)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *OrganizationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan OrganizationResourceModel
	var state OrganizationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.updateString("name", plan.Name, state.Name)
	attributes.updateString("description", plan.Description, state.Description)
	attributes.updateString("executionMode", plan.ExecutionMode, state.ExecutionMode)
	attributes.updateString("icon", plan.Icon, state.Icon)

	id := state.ID.ValueString()
	organization, err := r.client.Organizations.UpdateFields(ctx, id, terrakube.Patch{Attributes: attributes})
	if patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating organization", err)
		return
	}

	organization, err = readAfterPatch(organization, func() (*terrakube.Organization, error) {
		return r.client.Organizations.Get(ctx, id)
	})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading organization after update", err)
		return
	}

	plan.ID = types.StringValue(id)
	setOrganizationState(&plan, organization)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete disables the organization, since Terrakube cannot delete one, and renames it so the
// name can be used again.
func (r *OrganizationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data OrganizationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name, err := randomSoftDeletedName(data.Name.ValueString(), 32)
	if err != nil {
		resp.Diagnostics.AddError("Error generating random string to delete organization", fmt.Sprintf("Error generating random string to delete organization: %s", err))
		return
	}

	attributes := patchAttributes{
		"disabled":    true,
		"name":        name,
		"description": "Deleted from terraform provider",
	}
	attributes.createString("executionMode", data.ExecutionMode)

	_, err = r.client.Organizations.UpdateFields(ctx, data.ID.ValueString(), terrakube.Patch{Attributes: attributes})
	if patchFailed(err) && !terrakube.IsNotFound(err) {
		reportClientError(&resp.Diagnostics, "deleting organization", err)
	}
}

func (r *OrganizationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// setOrganizationState copies the attributes Terrakube returned into the model.
func setOrganizationState(model *OrganizationResourceModel, organization *terrakube.Organization) {
	model.Name = types.StringValue(organization.Name)
	model.Description = types.StringPointerValue(organization.Description)
	model.ExecutionMode = types.StringValue(organization.ExecutionMode)
	model.Icon = types.StringPointerValue(organization.Icon)
}
