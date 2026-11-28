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

var _ resource.Resource = &PolicySetParameterResource{}
var _ resource.ResourceWithConfigure = &PolicySetParameterResource{}
var _ resource.ResourceWithImportState = &PolicySetParameterResource{}

type PolicySetParameterResource struct {
	client *terrakube.Client
}

type PolicySetParameterResourceModel struct {
	ID          types.String `tfsdk:"id"`
	PolicySetId types.String `tfsdk:"policy_set_id"`
	Key         types.String `tfsdk:"key"`
	Value       types.String `tfsdk:"value"`
	Description types.String `tfsdk:"description"`
}

func NewPolicySetParameterResource() resource.Resource {
	return &PolicySetParameterResource{}
}

func (r *PolicySetParameterResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_policy_set_parameter"
}

func (r *PolicySetParameterResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create and manage an OPA Policy Set Parameter in Terrakube. Parameters are key-value inputs passed directly to Rego policy evaluation at runtime under `data.terrakube.inputs.<key>`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Parameter ID (UUID)",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"policy_set_id": schema.StringAttribute{
				Required:    true,
				Description: "Target Policy Set ID. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"key": schema.StringAttribute{
				Required:    true,
				Description: "Parameter key identifier, referenced in Rego as data.terrakube.inputs.<key>",
			},
			"value": schema.StringAttribute{
				Required:    true,
				Description: "Parameter value (supports strings, numbers, booleans, and JSON structures)",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Parameter description or documentation",
			},
		},
	}
}

func (r *PolicySetParameterResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Policy Set Parameter Resource")
}

func (r *PolicySetParameterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PolicySetParameterResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.createString("key", plan.Key)
	attributes.createString("value", plan.Value)
	attributes.createString("description", plan.Description)

	policySetID := plan.PolicySetId.ValueString()
	patch := terrakube.Patch{
		Attributes:    attributes,
		Relationships: map[string]terrakube.Relationship{"policySet": {Type: "policy_set", ID: &policySetID}},
	}
	parameter, err := r.client.PolicySetParameters.CreateFieldsByPolicySet(ctx, policySetID, patch)
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating policy set parameter", err)
		return
	}

	setPolicySetParameterState(&plan, parameter)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PolicySetParameterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PolicySetParameterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	parameter, err := r.client.PolicySetParameters.GetByPolicySet(ctx, state.PolicySetId.ValueString(), state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading policy set parameter", err)
		return
	}

	setPolicySetParameterState(&state, parameter)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update works from the plan, as it always has: key and value are always sent. Of the prior state
// only the description is read, so a description removed from the configuration is cleared with
// null; with no readable prior state it counts as null and nothing is cleared. The state is taken
// from the PATCH result, which UpdateFieldsByPolicySet reads back when Terrakube answers 204; when
// that read-back fails the update still stands and the plan is kept.
func (r *PolicySetParameterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan PolicySetParameterResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	priorDescription := types.StringNull()
	if !req.State.Raw.IsNull() {
		var prior types.String
		if diags := req.State.GetAttribute(ctx, path.Root("description"), &prior); !diags.HasError() {
			priorDescription = prior
		}
	}

	attributes := patchAttributes{}
	attributes.createString("key", plan.Key)
	attributes.createString("value", plan.Value)
	attributes.updateString("description", plan.Description, priorDescription)

	parameter, err := r.client.PolicySetParameters.UpdateFieldsByPolicySet(ctx, plan.PolicySetId.ValueString(), plan.ID.ValueString(), terrakube.Patch{Attributes: attributes})
	if patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating policy set parameter", err)
		return
	}
	if err == nil && parameter.ID != "" {
		setPolicySetParameterState(&plan, parameter)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PolicySetParameterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PolicySetParameterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.PolicySetParameters.DeleteByPolicySet(ctx, state.PolicySetId.ValueString(), state.ID.ValueString())
	if reportDeleteError(&resp.Diagnostics, "deleting policy set parameter", err) {
		return
	}

	tflog.Info(ctx, "Policy set parameter deleted", map[string]any{"id": state.ID.ValueString()})
}

func setPolicySetParameterState(model *PolicySetParameterResourceModel, parameter *terrakube.PolicySetParameter) {
	model.ID = types.StringValue(parameter.ID)
	model.Key = types.StringValue(stringOrEmpty(parameter.Key))
	model.Value = types.StringValue(stringOrEmpty(parameter.Value))
	model.Description = types.StringPointerValue(parameter.Description)
}

func (r *PolicySetParameterResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var idParts []string
	if strings.Contains(req.ID, "/") {
		idParts = strings.Split(req.ID, "/")
	} else {
		idParts = strings.Split(req.ID, ",")
	}

	if len(idParts) != 2 || idParts[0] == "" || idParts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format 'policy_set_id/id' or 'policy_set_id,id', Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("policy_set_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[1])...)
}
