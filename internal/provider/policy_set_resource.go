package provider

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"terraform-provider-terrakube/internal/client"

	"github.com/google/jsonapi"
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
)

var _ resource.Resource = &PolicySetResource{}
var _ resource.ResourceWithConfigure = &PolicySetResource{}
var _ resource.ResourceWithImportState = &PolicySetResource{}

type PolicySetResource struct {
	client   *http.Client
	endpoint string
	token    string
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
				Description: "Terrakube organization ID",
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
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Policy Set Resource Configure Type",
			fmt.Sprintf("Expected *TerrakubeConnectionData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	if providerData.InsecureHttpClient {
		if custom, ok := http.DefaultTransport.(*http.Transport); ok {
			customTransport := custom.Clone()
			customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
			r.client = &http.Client{Transport: customTransport}
		} else {
			r.client = &http.Client{}
		}
	} else {
		r.client = &http.Client{}
	}

	r.endpoint = providerData.Endpoint
	r.token = providerData.Token

	tflog.Debug(ctx, "Configuring Policy Set resource", map[string]any{"success": true})
}

func (r *PolicySetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PolicySetResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.PolicySetEntity{
		Name:             plan.Name.ValueString(),
		Description:      plan.Description.ValueStringPointer(),
		EnforcementLevel: strings.ToUpper(plan.EnforcementLevel.ValueString()),
		Global:           plan.Global.ValueBool(),
		Repository:       plan.Repository.ValueStringPointer(),
		Branch:           plan.Branch.ValueString(),
		Folder:           plan.Folder.ValueString(),
	}

	if !plan.ShadowEnforcementLevel.IsNull() && !plan.ShadowEnforcementLevel.IsUnknown() {
		shadow := strings.ToUpper(plan.ShadowEnforcementLevel.ValueString())
		bodyRequest.ShadowEnforcementLevel = &shadow
	}

	if !plan.OverrideTeam.IsNull() && !plan.OverrideTeam.IsUnknown() {
		team := plan.OverrideTeam.ValueString()
		bodyRequest.OverrideTeam = &team
	}

	if !plan.VcsId.IsNull() && !plan.VcsId.IsUnknown() && plan.VcsId.ValueString() != "" {
		bodyRequest.Vcs = &client.VcsEntity{ID: plan.VcsId.ValueString()}
	}

	if !plan.NotificationConfigurationId.IsNull() && !plan.NotificationConfigurationId.IsUnknown() && plan.NotificationConfigurationId.ValueString() != "" {
		bodyRequest.NotificationConfiguration = &client.NotificationConfigurationEntity{ID: plan.NotificationConfigurationId.ValueString()}
	}

	if !plan.OpaVersion.IsNull() && !plan.OpaVersion.IsUnknown() && plan.OpaVersion.ValueString() != "" {
		opaVer := plan.OpaVersion.ValueString()
		bodyRequest.OpaVersion = &opaVer
	}

	var out bytes.Buffer
	if err := jsonapi.MarshalPayload(&out, bodyRequest); err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	url := fmt.Sprintf("%s/api/v1/organization/%s/policySet", r.endpoint, plan.OrganizationId.ValueString())
	createReq, err := http.NewRequest(http.MethodPost, url, strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating policy set resource request", fmt.Sprintf("Error creating policy set resource request: %s", err))
		return
	}
	createReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	createReq.Header.Add("Content-Type", "application/vnd.api+json")

	createResp, err := r.client.Do(createReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing policy set create request", fmt.Sprintf("Error executing policy set create request: %s", err))
		return
	}
	defer createResp.Body.Close()

	bodyResponse, err := io.ReadAll(createResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading policy set response body", fmt.Sprintf("Error reading policy set response body: %s", err))
		return
	}

	if createResp.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error creating policy set", fmt.Sprintf("status: %v, body: %v", createResp.Status, string(bodyResponse)))
		return
	}

	newPolicySet := &client.PolicySetEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), newPolicySet); err != nil {
		resp.Diagnostics.AddError("Error unmarshalling policy set payload response", fmt.Sprintf("Error unmarshalling payload: %s", err))
		return
	}

	plan.ID = types.StringValue(newPolicySet.ID)
	plan.Name = types.StringValue(newPolicySet.Name)
	plan.Description = types.StringPointerValue(newPolicySet.Description)
	plan.EnforcementLevel = types.StringValue(strings.ToLower(newPolicySet.EnforcementLevel))
	if newPolicySet.ShadowEnforcementLevel != nil {
		plan.ShadowEnforcementLevel = types.StringValue(strings.ToLower(*newPolicySet.ShadowEnforcementLevel))
	} else {
		plan.ShadowEnforcementLevel = types.StringNull()
	}
	plan.OverrideTeam = types.StringPointerValue(newPolicySet.OverrideTeam)
	plan.Global = types.BoolValue(newPolicySet.Global)
	plan.Repository = types.StringPointerValue(newPolicySet.Repository)
	plan.Branch = types.StringValue(newPolicySet.Branch)
	plan.Folder = types.StringValue(newPolicySet.Folder)
	if newPolicySet.Vcs != nil {
		plan.VcsId = types.StringValue(newPolicySet.Vcs.ID)
	}
	if newPolicySet.NotificationConfiguration != nil {
		plan.NotificationConfigurationId = types.StringValue(newPolicySet.NotificationConfiguration.ID)
	}
	plan.OpaVersion = types.StringPointerValue(newPolicySet.OpaVersion)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PolicySetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PolicySetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	url := fmt.Sprintf("%s/api/v1/policy_set/%s", r.endpoint, state.ID.ValueString())
	readReq, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating policy set read request", fmt.Sprintf("Error: %s", err))
		return
	}
	readReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	readReq.Header.Add("Content-Type", "application/vnd.api+json")

	readResp, err := r.client.Do(readReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing policy set read request", fmt.Sprintf("Error: %s", err))
		return
	}
	defer readResp.Body.Close()

	if readResp.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Policy set not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	bodyResponse, err := io.ReadAll(readResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading policy set response body", fmt.Sprintf("Error: %s", err))
		return
	}

	if readResp.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error reading policy set", fmt.Sprintf("status: %v, body: %v", readResp.Status, string(bodyResponse)))
		return
	}

	policySet := &client.PolicySetEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), policySet); err != nil {
		resp.Diagnostics.AddError("Error unmarshalling policy set payload", fmt.Sprintf("Error: %s", err))
		return
	}

	state.ID = types.StringValue(policySet.ID)
	state.Name = types.StringValue(policySet.Name)
	state.Description = types.StringPointerValue(policySet.Description)
	state.EnforcementLevel = types.StringValue(strings.ToLower(policySet.EnforcementLevel))
	if policySet.ShadowEnforcementLevel != nil {
		state.ShadowEnforcementLevel = types.StringValue(strings.ToLower(*policySet.ShadowEnforcementLevel))
	} else {
		state.ShadowEnforcementLevel = types.StringNull()
	}
	state.OverrideTeam = types.StringPointerValue(policySet.OverrideTeam)
	state.Global = types.BoolValue(policySet.Global)
	state.Repository = types.StringPointerValue(policySet.Repository)
	state.Branch = types.StringValue(policySet.Branch)
	state.Folder = types.StringValue(policySet.Folder)
	if policySet.Vcs != nil {
		state.VcsId = types.StringValue(policySet.Vcs.ID)
	}
	if policySet.NotificationConfiguration != nil {
		state.NotificationConfigurationId = types.StringValue(policySet.NotificationConfiguration.ID)
	}
	state.OpaVersion = types.StringPointerValue(policySet.OpaVersion)

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

	bodyRequest := &client.PolicySetEntity{
		ID:               state.ID.ValueString(),
		Name:             plan.Name.ValueString(),
		Description:      plan.Description.ValueStringPointer(),
		EnforcementLevel: strings.ToUpper(plan.EnforcementLevel.ValueString()),
		Global:           plan.Global.ValueBool(),
		Repository:       plan.Repository.ValueStringPointer(),
		Branch:           plan.Branch.ValueString(),
		Folder:           plan.Folder.ValueString(),
	}

	if !plan.ShadowEnforcementLevel.IsNull() && !plan.ShadowEnforcementLevel.IsUnknown() {
		shadow := strings.ToUpper(plan.ShadowEnforcementLevel.ValueString())
		bodyRequest.ShadowEnforcementLevel = &shadow
	}

	if !plan.OverrideTeam.IsNull() && !plan.OverrideTeam.IsUnknown() {
		team := plan.OverrideTeam.ValueString()
		bodyRequest.OverrideTeam = &team
	}

	if !plan.VcsId.IsNull() && !plan.VcsId.IsUnknown() && plan.VcsId.ValueString() != "" {
		bodyRequest.Vcs = &client.VcsEntity{ID: plan.VcsId.ValueString()}
	}

	if !plan.NotificationConfigurationId.IsNull() && !plan.NotificationConfigurationId.IsUnknown() && plan.NotificationConfigurationId.ValueString() != "" {
		bodyRequest.NotificationConfiguration = &client.NotificationConfigurationEntity{ID: plan.NotificationConfigurationId.ValueString()}
	}

	if !plan.OpaVersion.IsNull() && !plan.OpaVersion.IsUnknown() && plan.OpaVersion.ValueString() != "" {
		opaVer := plan.OpaVersion.ValueString()
		bodyRequest.OpaVersion = &opaVer
	}

	var out bytes.Buffer
	if err := jsonapi.MarshalPayload(&out, bodyRequest); err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	url := fmt.Sprintf("%s/api/v1/policy_set/%s", r.endpoint, state.ID.ValueString())
	updateReq, err := http.NewRequest(http.MethodPatch, url, strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating policy set update request", fmt.Sprintf("Error: %s", err))
		return
	}
	updateReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	updateReq.Header.Add("Content-Type", "application/vnd.api+json")

	updateResp, err := r.client.Do(updateReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing policy set update request", fmt.Sprintf("Error: %s", err))
		return
	}
	defer updateResp.Body.Close()

	bodyResponse, err := io.ReadAll(updateResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading policy set update response body", fmt.Sprintf("Error: %s", err))
		return
	}

	if updateResp.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Policy set not found during update, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	if updateResp.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error updating policy set", fmt.Sprintf("status: %v, body: %v", updateResp.Status, string(bodyResponse)))
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.OrganizationId = types.StringValue(state.OrganizationId.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PolicySetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PolicySetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	url := fmt.Sprintf("%s/api/v1/policy_set/%s", r.endpoint, state.ID.ValueString())
	delReq, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating policy set delete request", fmt.Sprintf("Error: %s", err))
		return
	}
	delReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))

	delResp, err := r.client.Do(delReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing policy set delete request", fmt.Sprintf("Error: %s", err))
		return
	}
	defer delResp.Body.Close()

	if delResp.StatusCode >= http.StatusBadRequest && delResp.StatusCode != http.StatusNotFound {
		bodyResponse, _ := io.ReadAll(delResp.Body)
		resp.Diagnostics.AddError("Error deleting policy set", fmt.Sprintf("status: %v, body: %v", delResp.Status, string(bodyResponse)))
		return
	}

	tflog.Info(ctx, "Policy Set deleted", map[string]any{"id": state.ID.ValueString()})
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
