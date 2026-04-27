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
var _ resource.Resource = &WorkspaceAccessResource{}
var _ resource.ResourceWithImportState = &WorkspaceAccessResource{}
var _ resource.ResourceWithConfigValidators = &WorkspaceAccessResource{}

type WorkspaceAccessResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type WorkspaceAccessResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	OrganizationId  types.String `tfsdk:"organization_id"`
	WorkspaceId     types.String `tfsdk:"workspace_id"`
	ManageState     types.Bool   `tfsdk:"manage_state"`
	ManageWorkspace types.Bool   `tfsdk:"manage_workspace"`
	ManageJob       types.Bool   `tfsdk:"manage_job"`
	PlanJob         types.Bool   `tfsdk:"plan_job"`
	ApproveJob      types.Bool   `tfsdk:"approve_job"`
	Role            types.String `tfsdk:"role"`
}

func NewWorkspaceAccessResource() resource.Resource {
	return &WorkspaceAccessResource{}
}

func (r *WorkspaceAccessResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{rbacRoleConflictValidator{}}
}

func (r *WorkspaceAccessResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_access"
}

func (r *WorkspaceAccessResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage workspace access.",

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
			"workspace_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube workspace id",
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
				Description: "Allow to manage workspaces",
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

func (r *WorkspaceAccessResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Workspace Access Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Workspace Access resource", map[string]any{"success": true})
}

func (r *WorkspaceAccessResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceAccessResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.WorkspaceAccessEntity{
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

	workspaceAccessRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/access", r.endpoint, plan.OrganizationId.ValueString(), plan.WorkspaceId.ValueString()), strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace access resource request", fmt.Sprintf("Error creating workspace access resource request: %s", err))
		return
	}
	workspaceAccessRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceAccessRequest.Header.Add("Content-Type", "application/vnd.api+json")

	workspaceAccessResponse, err := r.client.Do(workspaceAccessRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace access resource request", fmt.Sprintf("Error executing workspace access resource request: %s", err))
		return
	}
	defer workspaceAccessResponse.Body.Close()

	bodyResponse, err := io.ReadAll(workspaceAccessResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading workspace access resource response body", fmt.Sprintf("Error reading workspace access resource response body: %s", err))
		return
	}

	if workspaceAccessResponse.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error creating workspace access", fmt.Sprintf("status: %v, body: %v", workspaceAccessResponse.Status, string(bodyResponse)))
		return
	}

	workspaceAccess := &client.WorkspaceAccessEntity{}

	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), workspaceAccess)
	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ManageState = types.BoolValue(workspaceAccess.ManageState)
	plan.ManageWorkspace = types.BoolValue(workspaceAccess.ManageWorkspace)
	plan.ManageJob = types.BoolValue(workspaceAccess.ManageJob)
	plan.PlanJob = types.BoolValue(workspaceAccess.PlanJob)
	plan.ApproveJob = types.BoolValue(workspaceAccess.ApproveJob)
	plan.Role = roleToState(workspaceAccess.Role)
	plan.ID = types.StringValue(workspaceAccess.ID)

	tflog.Info(ctx, "workspace access Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceAccessResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceAccessResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceAccessRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/access/%s", r.endpoint, state.OrganizationId.ValueString(), state.WorkspaceId.ValueString(), state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace access resource request", fmt.Sprintf("Error creating workspace access resource request: %s", err))
		return
	}
	workspaceAccessRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceAccessRequest.Header.Add("Content-Type", "application/vnd.api+json")

	workspaceAccessResponse, err := r.client.Do(workspaceAccessRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace access resource request", fmt.Sprintf("Error executing workspace access resource request: %s", err))
		return
	}
	defer workspaceAccessResponse.Body.Close()

	if workspaceAccessResponse.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Workspace access not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	bodyResponse, err := io.ReadAll(workspaceAccessResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading workspace access resource response body", fmt.Sprintf("Error reading workspace access resource response body: %s", err))
		return
	}

	if workspaceAccessResponse.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error reading workspace access", fmt.Sprintf("status: %v, body: %v", workspaceAccessResponse.Status, string(bodyResponse)))
		return
	}

	workspaceAccess := &client.WorkspaceAccessEntity{}

	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), workspaceAccess)
	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	state.ManageState = types.BoolValue(workspaceAccess.ManageState)
	state.ManageWorkspace = types.BoolValue(workspaceAccess.ManageWorkspace)
	state.ManageJob = types.BoolValue(workspaceAccess.ManageJob)
	state.PlanJob = types.BoolValue(workspaceAccess.PlanJob)
	state.ApproveJob = types.BoolValue(workspaceAccess.ApproveJob)
	state.Role = roleToState(workspaceAccess.Role)
	state.Name = types.StringValue(workspaceAccess.Name)
	state.ID = types.StringValue(workspaceAccess.ID)

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Workspace access Resource reading", map[string]any{"success": true})
}

func (r *WorkspaceAccessResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan WorkspaceAccessResourceModel
	var state WorkspaceAccessResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.WorkspaceAccessEntity{
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

	workspaceAccessReq, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/access/%s", r.endpoint, state.OrganizationId.ValueString(), state.WorkspaceId.ValueString(), state.ID.ValueString()), strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating Workspace access resource request", fmt.Sprintf("Error creating Workspace access resource request: %s", err))
		return
	}
	workspaceAccessReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceAccessReq.Header.Add("Content-Type", "application/vnd.api+json")

	workspaceAccessResponse, err := r.client.Do(workspaceAccessReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing Workspace access resource request", fmt.Sprintf("Error executing Workspace access resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(workspaceAccessResponse.Body)
	workspaceAccessResponse.Body.Close()
	if err != nil {
		resp.Diagnostics.AddError("Error reading Workspace access resource response body",
			fmt.Sprintf("Error reading Workspace access resource response body: %s", err))
		return
	}

	if workspaceAccessResponse.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Workspace access not found during update, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	if workspaceAccessResponse.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error updating workspace access", fmt.Sprintf("status: %v, body: %v", workspaceAccessResponse.Status, string(bodyResponse)))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"success": string(bodyResponse)})

	workspaceAccessReq, err = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/access/%s", r.endpoint, state.OrganizationId.ValueString(), state.WorkspaceId.ValueString(), state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating Workspace access resource request", fmt.Sprintf("Error creating Workspace access resource request: %s", err))
		return
	}
	workspaceAccessReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceAccessReq.Header.Add("Content-Type", "application/vnd.api+json")

	workspaceAccessResponse, err = r.client.Do(workspaceAccessReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing Workspace access resource request", fmt.Sprintf("Error executing Workspace access resource request: %s", err))
		return
	}
	defer workspaceAccessResponse.Body.Close()

	bodyResponse, err = io.ReadAll(workspaceAccessResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading Workspace access resource response body", fmt.Sprintf("Error reading Workspace access resource response body: %s", err))
		return
	}

	if workspaceAccessResponse.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Workspace access not found after update, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	if workspaceAccessResponse.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error reading workspace access after update", fmt.Sprintf("status: %v, body: %v", workspaceAccessResponse.Status, string(bodyResponse)))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	workspaceAccess := &client.WorkspaceAccessEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), workspaceAccess)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.ManageState = types.BoolValue(workspaceAccess.ManageState)
	plan.ManageWorkspace = types.BoolValue(workspaceAccess.ManageWorkspace)
	plan.ManageJob = types.BoolValue(workspaceAccess.ManageJob)
	plan.PlanJob = types.BoolValue(workspaceAccess.PlanJob)
	plan.ApproveJob = types.BoolValue(workspaceAccess.ApproveJob)
	plan.Role = roleToState(workspaceAccess.Role)
	plan.Name = types.StringValue(workspaceAccess.Name)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceAccessResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data WorkspaceAccessResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	workspaceRequest, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/access/%s", r.endpoint, data.OrganizationId.ValueString(), data.WorkspaceId.ValueString(), data.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating Workspace access resource request", fmt.Sprintf("Error creating Workspace access resource request: %s", err))
		return
	}
	workspaceRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))

	delResp, err := r.client.Do(workspaceRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing Workspace access resource request", fmt.Sprintf("Error executing Workspace access resource request: %s", err))
		return
	}
	defer delResp.Body.Close()
}

func (r *WorkspaceAccessResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) != 3 || idParts[0] == "" || idParts[1] == "" || idParts[2] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: 'organization_ID,workspace_ID,ID', Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), idParts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[2])...)
}
