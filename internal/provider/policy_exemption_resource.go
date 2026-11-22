package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	terrakube "github.com/terrakube-io/terrakube-go"
)

var _ resource.Resource = &PolicyExemptionResource{}
var _ resource.ResourceWithConfigure = &PolicyExemptionResource{}
var _ resource.ResourceWithImportState = &PolicyExemptionResource{}

type PolicyExemptionResource struct {
	client *terrakube.Client
}

type PolicyExemptionResourceModel struct {
	ID              types.String `tfsdk:"id"`
	OrganizationId  types.String `tfsdk:"organization_id"`
	PolicySetId     types.String `tfsdk:"policy_set_id"`
	RuleId          types.String `tfsdk:"rule_id"`
	TicketReference types.String `tfsdk:"ticket_reference"`
	Justification   types.String `tfsdk:"justification"`
	ExpiresAt       types.String `tfsdk:"expires_at"`
	WorkspaceId     types.String `tfsdk:"workspace_id"`
	ProjectId       types.String `tfsdk:"project_id"`
}

func NewPolicyExemptionResource() resource.Resource {
	return &PolicyExemptionResource{}
}

func (r *PolicyExemptionResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_policy_exemption"
}

func (r *PolicyExemptionResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create and manage a time-bounded OPA Policy Rule Exemption in Terrakube. Exemptions allow specific rules to be safely bypassed with an audit trail (ticket reference and justification) for designated workspaces or projects until an expiration date.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Exemption ID (UUID)",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization ID. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"policy_set_id": schema.StringAttribute{
				Required:    true,
				Description: "Target Policy Set ID containing the rule to exempt",
			},
			"rule_id": schema.StringAttribute{
				Required:    true,
				Description: "OPA Rego rule ID or package path (e.g. azure_apim_no_public_network)",
			},
			"ticket_reference": schema.StringAttribute{
				Required:    true,
				Description: "Ticketing system reference (e.g. Jira SEC-8842, ServiceNow RITM10293)",
			},
			"justification": schema.StringAttribute{
				Required:    true,
				Description: "Detailed justification explaining why the policy rule is exempted",
			},
			"expires_at": schema.StringAttribute{
				Optional:    true,
				Description: "Expiration timestamp in RFC3339 format (e.g. 2026-12-31T23:59:59Z). After this date, the exemption automatically deactivates.",
			},
			"workspace_id": schema.StringAttribute{
				Optional:    true,
				Description: "Optional workspace ID to restrict the exemption to a specific workspace",
			},
			"project_id": schema.StringAttribute{
				Optional:    true,
				Description: "Optional project ID to restrict the exemption to all workspaces in a project",
			},
		},
	}
}

func (r *PolicyExemptionResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Policy Exemption Resource")
}

func (r *PolicyExemptionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PolicyExemptionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.createString("ruleId", plan.RuleId)
	attributes.createString("ticketReference", plan.TicketReference)
	attributes.createString("justification", plan.Justification)
	attributes.createString("expiresAt", plan.ExpiresAt)

	patch := terrakube.Patch{Attributes: attributes, Relationships: policyExemptionRelationships(plan)}
	exemption, err := r.client.PolicyExemptions.CreateFields(ctx, plan.OrganizationId.ValueString(), patch)
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating policy exemption", err)
		return
	}

	setPolicyExemptionState(&plan, exemption)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PolicyExemptionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PolicyExemptionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	exemption, err := r.client.PolicyExemptions.GetByID(ctx, state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Policy exemption not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading policy exemption", err)
		return
	}

	setPolicyExemptionState(&state, exemption)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *PolicyExemptionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan PolicyExemptionResourceModel
	var state PolicyExemptionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.updateString("ruleId", plan.RuleId, state.RuleId)
	attributes.updateString("ticketReference", plan.TicketReference, state.TicketReference)
	attributes.updateString("justification", plan.Justification, state.Justification)
	attributes.updateString("expiresAt", plan.ExpiresAt, state.ExpiresAt)

	id := state.ID.ValueString()
	patch := terrakube.Patch{Attributes: attributes, Relationships: policyExemptionRelationships(plan)}
	_, err := r.client.PolicyExemptions.UpdateFieldsByID(ctx, id, patch)
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Policy exemption not found during update, removing from state", map[string]any{"id": id})
		resp.State.RemoveResource(ctx)
		return
	}
	if patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating policy exemption", err)
		return
	}

	exemption, err := r.client.PolicyExemptions.GetByID(ctx, id)
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Policy exemption not found after update, removing from state", map[string]any{"id": id})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading policy exemption after update", err)
		return
	}

	// As before, a workspace or project link removed from the configuration is not cleared on the
	// server, so the state keeps the planned IDs rather than the links Terrakube still has.
	workspaceID, projectID := plan.WorkspaceId, plan.ProjectId
	setPolicyExemptionState(&plan, exemption)
	plan.WorkspaceId, plan.ProjectId = workspaceID, projectID
	plan.OrganizationId = state.OrganizationId
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PolicyExemptionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PolicyExemptionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.PolicyExemptions.DeleteByID(ctx, state.ID.ValueString())
	if err != nil && !terrakube.IsNotFound(err) {
		reportClientError(&resp.Diagnostics, "deleting policy exemption", err)
		return
	}

	tflog.Info(ctx, "Policy exemption deleted", map[string]any{"id": state.ID.ValueString()})
}

// policyExemptionRelationships links the exemption to its policy set, and to its workspace and
// project when they are set. As before, a workspace or project link is never cleared.
func policyExemptionRelationships(model PolicyExemptionResourceModel) map[string]terrakube.Relationship {
	policySetID := model.PolicySetId.ValueString()
	relationships := map[string]terrakube.Relationship{"policySet": {Type: "policy_set", ID: &policySetID}}
	if !model.WorkspaceId.IsNull() && !model.WorkspaceId.IsUnknown() && model.WorkspaceId.ValueString() != "" {
		workspaceID := model.WorkspaceId.ValueString()
		relationships["workspace"] = terrakube.Relationship{Type: "workspace", ID: &workspaceID}
	}
	if !model.ProjectId.IsNull() && !model.ProjectId.IsUnknown() && model.ProjectId.ValueString() != "" {
		projectID := model.ProjectId.ValueString()
		relationships["project"] = terrakube.Relationship{Type: "project", ID: &projectID}
	}
	return relationships
}

func setPolicyExemptionState(model *PolicyExemptionResourceModel, exemption *terrakube.PolicyExemption) {
	model.ID = types.StringValue(exemption.ID)
	model.RuleId = types.StringValue(stringOrEmpty(exemption.RuleID))
	model.TicketReference = types.StringValue(stringOrEmpty(exemption.TicketReference))
	model.Justification = types.StringValue(stringOrEmpty(exemption.Justification))
	model.ExpiresAt = expiresAtState(model.ExpiresAt, exemption.ExpiresAt)
	if exemption.PolicySet != nil {
		model.PolicySetId = types.StringValue(exemption.PolicySet.ID)
	}
	if exemption.Workspace != nil {
		model.WorkspaceId = types.StringValue(exemption.Workspace.ID)
	}
	if exemption.Project != nil {
		model.ProjectId = types.StringValue(exemption.Project.ID)
	}
}

// expiresAtState is the expires_at value for an expiry time from the API. expires_at is sent to
// Terrakube exactly as written, but terrakube-go decodes the answer into a time, so the text the
// server returned is gone. The current text is kept when it names the same instant, so a value
// written as 2026-12-31T23:59:59Z stays that way; otherwise the time is written in RFC 3339.
func expiresAtState(current types.String, expiresAt *time.Time) types.String {
	if expiresAt == nil {
		return types.StringNull()
	}
	if !current.IsNull() && !current.IsUnknown() {
		if written, err := time.Parse(time.RFC3339, current.ValueString()); err == nil && written.Equal(*expiresAt) {
			return current
		}
	}
	return types.StringValue(expiresAt.Format(time.RFC3339Nano))
}

func (r *PolicyExemptionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) == 2 {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), idParts[0])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[1])...)
		return
	}

	if len(idParts) == 1 && idParts[0] != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[0])...)
		return
	}

	resp.Diagnostics.AddError(
		"Unexpected Import Identifier",
		fmt.Sprintf("Expected import identifier with format: 'organization_id,id' or 'id', Got: %q", req.ID),
	)
}
