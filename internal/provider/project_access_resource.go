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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &ProjectAccessResource{}
var _ resource.ResourceWithImportState = &ProjectAccessResource{}
var _ resource.ResourceWithConfigValidators = &ProjectAccessResource{}

type ProjectAccessResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type ProjectAccessResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	OrganizationId  types.String `tfsdk:"organization_id"`
	ProjectId       types.String `tfsdk:"project_id"`
	ManageState     types.Bool   `tfsdk:"manage_state"`
	ManageWorkspace types.Bool   `tfsdk:"manage_workspace"`
	ManageJob       types.Bool   `tfsdk:"manage_job"`
	PlanJob         types.Bool   `tfsdk:"plan_job"`
	ApproveJob      types.Bool   `tfsdk:"approve_job"`
	Role            types.String `tfsdk:"role"`
}

func NewProjectAccessResource() resource.Resource {
	return &ProjectAccessResource{}
}

func (r *ProjectAccessResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{rbacRoleConflictValidator{}}
}

func (r *ProjectAccessResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_access"
}

func (r *ProjectAccessResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage project access. Grants a team RBAC permissions scoped to a project (and therefore to every workspace within it).",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Access Id",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization id",
			},
			"project_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube project id",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Team name",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"manage_state": schema.BoolAttribute{
				Optional:    true,
				Description: "Allow to manage Terraform/OpenTofu state",
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"manage_job": schema.BoolAttribute{
				Optional:    true,
				Description: "Allow to manage and trigger jobs. Legacy field — in RBAC v2, plan_job/approve_job inherit from this when unset.",
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"manage_workspace": schema.BoolAttribute{
				Optional:    true,
				Description: "Allow to create, update, and delete workspaces within the project",
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"plan_job": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Allow queuing plans (RBAC v2). Inherits manage_job when not set. Only used when role is unset or \"custom\". Note: inheritance only applies on create/update — imported resources retain the remote value.",
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"approve_job": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Allow approving/applying runs (RBAC v2). Inherits manage_job when not set. Only used when role is unset or \"custom\". Note: inheritance only applies on create/update — imported resources retain the remote value.",
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"role": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Predefined role: admin (all permissions), write (plan+apply+workspace+state), plan (plan only), read (read only), or custom (use boolean flags). When set to a non-custom value, overrides individual boolean flags. Leave unset to use boolean flags.",
				Validators: []validator.String{
					stringvalidator.OneOf("admin", "write", "plan", "read", "custom"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *ProjectAccessResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Project Access Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Project Access resource", map[string]any{"success": true})
}

func (r *ProjectAccessResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ProjectAccessResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.ProjectAccessEntity{
		ManageState:     plan.ManageState.ValueBool(),
		ManageWorkspace: plan.ManageWorkspace.ValueBool(),
		ManageJob:       plan.ManageJob.ValueBool(),
		PlanJob:         resolveJobFlag(plan.PlanJob, plan.ManageJob),
		ApproveJob:      resolveJobFlag(plan.ApproveJob, plan.ManageJob),
		Name:            plan.Name.ValueString(),
	}

	if !plan.Role.IsNull() && !plan.Role.IsUnknown() {
		role := plan.Role.ValueString()
		bodyRequest.Role = &role
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	projectAccessRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/organization/%s/project/%s/projectAccess", r.endpoint, plan.OrganizationId.ValueString(), plan.ProjectId.ValueString()), strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating project access resource request", fmt.Sprintf("Error creating project access resource request: %s", err))
		return
	}
	projectAccessRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	projectAccessRequest.Header.Add("Content-Type", "application/vnd.api+json")

	projectAccessResponse, err := r.client.Do(projectAccessRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing project access resource request", fmt.Sprintf("Error executing project access resource request: %s", err))
		return
	}
	defer projectAccessResponse.Body.Close()

	bodyResponse, err := io.ReadAll(projectAccessResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading project access resource response body", fmt.Sprintf("Error reading project access resource response body: %s", err))
		return
	}

	if projectAccessResponse.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error creating project access", fmt.Sprintf("status: %v, body: %v", projectAccessResponse.Status, string(bodyResponse)))
		return
	}

	projectAccess := &client.ProjectAccessEntity{}

	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), projectAccess)
	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ManageState = types.BoolValue(projectAccess.ManageState)
	plan.ManageWorkspace = types.BoolValue(projectAccess.ManageWorkspace)
	plan.ManageJob = types.BoolValue(projectAccess.ManageJob)
	plan.PlanJob = types.BoolValue(projectAccess.PlanJob)
	plan.ApproveJob = types.BoolValue(projectAccess.ApproveJob)
	plan.Role = roleToState(projectAccess.Role)
	plan.ID = types.StringValue(projectAccess.ID)

	tflog.Info(ctx, "project access Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ProjectAccessResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ProjectAccessResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectAccessRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/project/%s/projectAccess/%s", r.endpoint, state.OrganizationId.ValueString(), state.ProjectId.ValueString(), state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating project access resource request", fmt.Sprintf("Error creating project access resource request: %s", err))
		return
	}
	projectAccessRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	projectAccessRequest.Header.Add("Content-Type", "application/vnd.api+json")

	projectAccessResponse, err := r.client.Do(projectAccessRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing project access resource request", fmt.Sprintf("Error executing project access resource request: %s", err))
		return
	}
	defer projectAccessResponse.Body.Close()

	if projectAccessResponse.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Project access not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	bodyResponse, err := io.ReadAll(projectAccessResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading project access resource response body", fmt.Sprintf("Error reading project access resource response body: %s", err))
		return
	}

	if projectAccessResponse.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error reading project access", fmt.Sprintf("status: %v, body: %v", projectAccessResponse.Status, string(bodyResponse)))
		return
	}

	projectAccess := &client.ProjectAccessEntity{}

	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), projectAccess)
	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	state.ManageState = types.BoolValue(projectAccess.ManageState)
	state.ManageWorkspace = types.BoolValue(projectAccess.ManageWorkspace)
	state.ManageJob = types.BoolValue(projectAccess.ManageJob)
	state.PlanJob = types.BoolValue(projectAccess.PlanJob)
	state.ApproveJob = types.BoolValue(projectAccess.ApproveJob)
	state.Role = roleToState(projectAccess.Role)
	state.Name = types.StringValue(projectAccess.Name)
	state.ID = types.StringValue(projectAccess.ID)

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Project access Resource reading", map[string]any{"success": true})
}

func (r *ProjectAccessResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan ProjectAccessResourceModel
	var state ProjectAccessResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.ProjectAccessEntity{
		ManageState:     plan.ManageState.ValueBool(),
		ManageWorkspace: plan.ManageWorkspace.ValueBool(),
		ManageJob:       plan.ManageJob.ValueBool(),
		PlanJob:         resolveJobFlag(plan.PlanJob, plan.ManageJob),
		ApproveJob:      resolveJobFlag(plan.ApproveJob, plan.ManageJob),
		Name:            plan.Name.ValueString(),
		ID:              state.ID.ValueString(),
	}

	if !plan.Role.IsNull() && !plan.Role.IsUnknown() {
		role := plan.Role.ValueString()
		bodyRequest.Role = &role
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	projectAccessReq, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/project/%s/projectAccess/%s", r.endpoint, state.OrganizationId.ValueString(), state.ProjectId.ValueString(), state.ID.ValueString()), strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating project access resource request", fmt.Sprintf("Error creating project access resource request: %s", err))
		return
	}
	projectAccessReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	projectAccessReq.Header.Add("Content-Type", "application/vnd.api+json")

	projectAccessResponse, err := r.client.Do(projectAccessReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing project access resource request", fmt.Sprintf("Error executing project access resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(projectAccessResponse.Body)
	projectAccessResponse.Body.Close()
	if err != nil {
		resp.Diagnostics.AddError("Error reading project access resource response body",
			fmt.Sprintf("Error reading project access resource response body: %s", err))
		return
	}

	if projectAccessResponse.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Project access not found during update, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	if projectAccessResponse.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error updating project access", fmt.Sprintf("status: %v, body: %v", projectAccessResponse.Status, string(bodyResponse)))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"success": string(bodyResponse)})

	projectAccessReq, err = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/project/%s/projectAccess/%s", r.endpoint, state.OrganizationId.ValueString(), state.ProjectId.ValueString(), state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating project access resource request", fmt.Sprintf("Error creating project access resource request: %s", err))
		return
	}
	projectAccessReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	projectAccessReq.Header.Add("Content-Type", "application/vnd.api+json")

	projectAccessResponse, err = r.client.Do(projectAccessReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing project access resource request", fmt.Sprintf("Error executing project access resource request: %s", err))
		return
	}
	defer projectAccessResponse.Body.Close()

	bodyResponse, err = io.ReadAll(projectAccessResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading project access resource response body", fmt.Sprintf("Error reading project access resource response body: %s", err))
		return
	}

	if projectAccessResponse.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Project access not found after update, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	if projectAccessResponse.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error reading project access after update", fmt.Sprintf("status: %v, body: %v", projectAccessResponse.Status, string(bodyResponse)))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	projectAccess := &client.ProjectAccessEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), projectAccess)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.ManageState = types.BoolValue(projectAccess.ManageState)
	plan.ManageWorkspace = types.BoolValue(projectAccess.ManageWorkspace)
	plan.ManageJob = types.BoolValue(projectAccess.ManageJob)
	plan.PlanJob = types.BoolValue(projectAccess.PlanJob)
	plan.ApproveJob = types.BoolValue(projectAccess.ApproveJob)
	plan.Role = roleToState(projectAccess.Role)
	plan.Name = types.StringValue(projectAccess.Name)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ProjectAccessResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ProjectAccessResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	projectAccessRequest, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/organization/%s/project/%s/projectAccess/%s", r.endpoint, data.OrganizationId.ValueString(), data.ProjectId.ValueString(), data.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating project access resource request", fmt.Sprintf("Error creating project access resource request: %s", err))
		return
	}
	projectAccessRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))

	delResp, err := r.client.Do(projectAccessRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing project access resource request", fmt.Sprintf("Error executing project access resource request: %s", err))
		return
	}
	defer delResp.Body.Close()
}

func (r *ProjectAccessResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) != 3 || idParts[0] == "" || idParts[1] == "" || idParts[2] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: 'organization_ID,project_ID,ID', Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), idParts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[2])...)
}
