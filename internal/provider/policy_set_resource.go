package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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

var _ resource.Resource = &PolicySetResource{}
var _ resource.ResourceWithConfigure = &PolicySetResource{}
var _ resource.ResourceWithImportState = &PolicySetResource{}

type PolicySetResource struct {
	client *terrakube.Client
}

type PolicySetResourceModel struct {
	ID                          types.String `tfsdk:"id"`
	OrganizationId              types.String `tfsdk:"organization_id"`
	Name                        types.String `tfsdk:"name"`
	Description                 types.String `tfsdk:"description"`
	EnforcementLevel            types.String `tfsdk:"enforcement_level"`
	ShadowEnforcementLevel      types.String `tfsdk:"shadow_enforcement_level"`
	OverrideTeam                types.String `tfsdk:"override_team"`
	Global                      types.Bool   `tfsdk:"global"`
	VcsId                       types.String `tfsdk:"vcs_id"`
	Repository                  types.String `tfsdk:"repository"`
	Branch                      types.String `tfsdk:"branch"`
	Folder                      types.String `tfsdk:"folder"`
	NotificationConfigurationId types.String `tfsdk:"notification_configuration_id"`
	OpaVersion                  types.String `tfsdk:"opa_version"`
}

func NewPolicySetResource() resource.Resource {
	return &PolicySetResource{}
}

func (r *PolicySetResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_policy_set"
}

func (r *PolicySetResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create and manage an OPA Policy Set in Terrakube. Policy sets define bundles of Open Policy Agent (Rego) policies and guardrails linked to a Git VCS repository, scoped globally or attached to specific workspaces, projects, or tags.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Policy Set ID (UUID)",
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
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Policy set name, unique within the organization",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Description of the policy set and guardrails enforced",
			},
			"enforcement_level": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("hard_mandatory"),
				Description: "Enforcement level: hard_mandatory (fails plan/apply), soft_mandatory (allows authorized override), or advisory (warning only)",
				Validators: []validator.String{
					stringvalidator.OneOf("hard_mandatory", "soft_mandatory", "advisory", "HARD_MANDATORY", "SOFT_MANDATORY", "ADVISORY"),
				},
			},
			"shadow_enforcement_level": schema.StringAttribute{
				Optional:    true,
				Description: "Optional shadow enforcement level for dark-launching or staging policies without impacting real runs",
				Validators: []validator.String{
					stringvalidator.OneOf("hard_mandatory", "soft_mandatory", "advisory", "HARD_MANDATORY", "SOFT_MANDATORY", "ADVISORY"),
				},
			},
			"override_team": schema.StringAttribute{
				Optional:    true,
				Description: "Team name authorized to override soft_mandatory violations in CLI and UI",
			},
			"global": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether this policy set applies globally to all workspaces in the organization",
			},
			"vcs_id": schema.StringAttribute{
				Optional:    true,
				Description: "VCS connection ID for private policy repositories",
			},
			"repository": schema.StringAttribute{
				Optional:    true,
				Description: "Git repository URL containing the OPA Rego policies",
			},
			"branch": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("main"),
				Description: "Git branch or SemVer release tag (e.g. v2.1.0)",
			},
			"folder": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("/"),
				Description: "Subfolder within repository where Rego bundles are stored",
			},
			"notification_configuration_id": schema.StringAttribute{
				Optional:    true,
				Description: "Notification configuration ID to alert on policy violations",
			},
			"opa_version": schema.StringAttribute{
				Optional:    true,
				Description: "OPA binary version to execute policies (e.g. 0.68.0)",
			},
		},
	}
}

func (r *PolicySetResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Policy Set Resource")
}

func (r *PolicySetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PolicySetResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.createString("name", plan.Name)
	attributes.createString("description", plan.Description)
	attributes.createString("enforcementLevel", upperString(plan.EnforcementLevel))
	attributes.createString("shadowEnforcementLevel", upperString(plan.ShadowEnforcementLevel))
	attributes.createString("overrideTeam", plan.OverrideTeam)
	attributes.createBool("global", plan.Global)
	attributes.createString("repository", plan.Repository)
	updateNonEmptyString(attributes, "branch", plan.Branch, types.StringNull())
	updateNonEmptyString(attributes, "folder", plan.Folder, types.StringNull())
	updateNonEmptyString(attributes, "opaVersion", plan.OpaVersion, types.StringNull())

	patch := terrakube.Patch{Attributes: attributes, Relationships: policySetRelationships(plan)}
	policySet, err := r.client.PolicySets.CreateFields(ctx, plan.OrganizationId.ValueString(), patch)
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating policy set", err)
		return
	}

	setPolicySetState(&plan, policySet)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PolicySetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PolicySetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	policySet, err := r.client.PolicySets.GetByID(ctx, state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Policy set not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading policy set", err)
		return
	}

	setPolicySetState(&state, policySet)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *PolicySetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan PolicySetResourceModel
	var state PolicySetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.updateString("name", plan.Name, state.Name)
	attributes.updateString("description", plan.Description, state.Description)
	attributes.updateString("enforcementLevel", upperString(plan.EnforcementLevel), state.EnforcementLevel)
	attributes.updateString("shadowEnforcementLevel", upperString(plan.ShadowEnforcementLevel), state.ShadowEnforcementLevel)
	attributes.updateString("overrideTeam", plan.OverrideTeam, state.OverrideTeam)
	attributes.updateBool("global", plan.Global, state.Global)
	attributes.updateString("repository", plan.Repository, state.Repository)
	updateNonEmptyString(attributes, "branch", plan.Branch, state.Branch)
	updateNonEmptyString(attributes, "folder", plan.Folder, state.Folder)
	updateNonEmptyString(attributes, "opaVersion", plan.OpaVersion, state.OpaVersion)

	id := state.ID.ValueString()
	patch := terrakube.Patch{Attributes: attributes, Relationships: policySetRelationships(plan)}
	policySet, err := r.client.PolicySets.UpdateFieldsByID(ctx, id, patch)
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Policy set not found during update, removing from state", map[string]any{"id": id})
		resp.State.RemoveResource(ctx)
		return
	}
	if patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating policy set", err)
		return
	}

	policySet, err = readAfterPatch(policySet, func() (*terrakube.PolicySet, error) {
		return r.client.PolicySets.GetByID(ctx, id)
	})
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Policy set not found after update, removing from state", map[string]any{"id": id})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading policy set after update", err)
		return
	}

	// A VCS or notification link removed from the configuration is not sent, and Terrakube does not
	// clear a link that is left out, so the state keeps the planned IDs rather than the links
	// Terrakube still has. branch, folder and opa_version set to "" are left out of the request, so
	// Terrakube keeps its value; the state keeps the planned "" too.
	vcsID, notificationID := plan.VcsId, plan.NotificationConfigurationId
	branch, folder, opaVersion := plan.Branch, plan.Folder, plan.OpaVersion
	setPolicySetState(&plan, policySet)
	plan.VcsId, plan.NotificationConfigurationId = vcsID, notificationID
	for _, kept := range []struct {
		planned types.String
		target  *types.String
	}{{branch, &plan.Branch}, {folder, &plan.Folder}, {opaVersion, &plan.OpaVersion}} {
		if isEmptyString(kept.planned) {
			*kept.target = kept.planned
		}
	}
	plan.OrganizationId = state.OrganizationId
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PolicySetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PolicySetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.PolicySets.DeleteByID(ctx, state.ID.ValueString())
	if reportDeleteError(&resp.Diagnostics, "deleting policy set", err) {
		return
	}

	tflog.Info(ctx, "Policy Set deleted", map[string]any{"id": state.ID.ValueString()})
}

// policySetRelationships links the policy set to its VCS connection and notification configuration
// when they are set. A link is never cleared: an ID removed from the configuration is not sent.
func policySetRelationships(model PolicySetResourceModel) map[string]terrakube.Relationship {
	relationships := map[string]terrakube.Relationship{}
	if !model.VcsId.IsNull() && !model.VcsId.IsUnknown() && model.VcsId.ValueString() != "" {
		vcsID := model.VcsId.ValueString()
		relationships["vcs"] = terrakube.Relationship{Type: "vcs", ID: &vcsID}
	}
	if !model.NotificationConfigurationId.IsNull() && !model.NotificationConfigurationId.IsUnknown() && model.NotificationConfigurationId.ValueString() != "" {
		notificationID := model.NotificationConfigurationId.ValueString()
		relationships["notificationConfiguration"] = terrakube.Relationship{Type: "notification_configuration", ID: &notificationID}
	}
	return relationships
}

// setPolicySetState copies a policy set from the API into the model, which holds the plan or the
// prior state. Terrakube stores enforcement levels in upper case; the schema uses lower case but
// also accepts upper case, so a level that matches the model's ignoring case is kept as written.
func setPolicySetState(model *PolicySetResourceModel, policySet *terrakube.PolicySet) {
	model.ID = types.StringValue(policySet.ID)
	model.Name = types.StringValue(stringOrEmpty(policySet.Name))
	model.Description = types.StringPointerValue(policySet.Description)
	model.EnforcementLevel = enforcementLevelState(model.EnforcementLevel, stringOrEmpty(policySet.EnforcementLevel))
	if policySet.ShadowEnforcementLevel != nil {
		model.ShadowEnforcementLevel = enforcementLevelState(model.ShadowEnforcementLevel, *policySet.ShadowEnforcementLevel)
	} else {
		model.ShadowEnforcementLevel = types.StringNull()
	}
	model.OverrideTeam = types.StringPointerValue(policySet.OverrideTeam)
	model.Global = types.BoolValue(policySet.Global)
	model.Repository = types.StringPointerValue(policySet.Repository)
	model.Branch = types.StringValue(stringOrEmpty(policySet.Branch))
	model.Folder = types.StringValue(stringOrEmpty(policySet.Folder))
	if policySet.VCS != nil {
		model.VcsId = types.StringValue(policySet.VCS.ID)
	}
	if policySet.NotificationConfiguration != nil {
		model.NotificationConfigurationId = types.StringValue(policySet.NotificationConfiguration.ID)
	}
	// An empty opa_version stays "" while Terrakube has no version, instead of turning into null.
	// An empty branch or folder already stays "".
	if !isEmptyString(model.OpaVersion) || stringOrEmpty(policySet.OpaVersion) != "" {
		model.OpaVersion = types.StringPointerValue(policySet.OpaVersion)
	}
}

// enforcementLevelState is the state value for an enforcement level from the API: the current
// value when it names the same level ignoring case, otherwise the API's level in lower case.
func enforcementLevelState(current types.String, level string) types.String {
	if !current.IsNull() && !current.IsUnknown() && strings.EqualFold(current.ValueString(), level) {
		return current
	}
	return types.StringValue(strings.ToLower(level))
}

// isEmptyString reports whether value is known and "".
func isEmptyString(value types.String) bool {
	return !value.IsNull() && !value.IsUnknown() && value.ValueString() == ""
}

// updateNonEmptyString is updateString, except that an empty value is left out, so an empty
// branch, folder or OPA version leaves Terrakube's value unchanged.
func updateNonEmptyString(attributes patchAttributes, name string, value, prior types.String) {
	if isEmptyString(value) {
		return
	}
	attributes.updateString(name, value, prior)
}

// upperString is value in upper case. A null or unknown value is returned as it is.
func upperString(value types.String) types.String {
	if value.IsNull() || value.IsUnknown() {
		return value
	}
	return types.StringValue(strings.ToUpper(value.ValueString()))
}

func (r *PolicySetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
