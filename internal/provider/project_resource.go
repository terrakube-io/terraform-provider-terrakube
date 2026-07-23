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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &ProjectResource{}
var _ resource.ResourceWithImportState = &ProjectResource{}

type ProjectResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type ProjectResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Description    types.String `tfsdk:"description"`
}

func NewProjectResource() resource.Resource {
	return &ProjectResource{}
}

func (r *ProjectResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (r *ProjectResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a project and bind it to an organization. Projects group workspaces together and enable project-scoped team access via `terrakube_project_access`.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Project Id",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization id",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Project name, unique within the organization",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Project description",
			},
		},
	}
}

func (r *ProjectResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Project Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Project resource", map[string]any{"success": true})
}

func (r *ProjectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ProjectResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.ProjectEntity{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueStringPointer(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	projectRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/organization/%s/project", r.endpoint, plan.OrganizationId.ValueString()), strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating project resource request", fmt.Sprintf("Error creating project resource request: %s", err))
		return
	}
	projectRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	projectRequest.Header.Add("Content-Type", "application/vnd.api+json")

	projectResponse, err := r.client.Do(projectRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing project resource request", fmt.Sprintf("Error executing project resource request: %s", err))
		return
	}
	defer projectResponse.Body.Close()

	bodyResponse, err := io.ReadAll(projectResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading project resource response body", fmt.Sprintf("Error reading project resource response body: %s", err))
		return
	}

	if projectResponse.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error creating project", fmt.Sprintf("status: %v, body: %v", projectResponse.Status, string(bodyResponse)))
		return
	}

	newProject := &client.ProjectEntity{}

	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), newProject)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	plan.ID = types.StringValue(newProject.ID)
	plan.Name = types.StringValue(newProject.Name)
	plan.Description = types.StringPointerValue(newProject.Description)

	tflog.Info(ctx, "Project Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ProjectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ProjectResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/project/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating project resource request", fmt.Sprintf("Error creating project resource request: %s", err))
		return
	}
	projectRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	projectRequest.Header.Add("Content-Type", "application/vnd.api+json")

	projectResponse, err := r.client.Do(projectRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing project resource request", fmt.Sprintf("Error executing project resource request: %s", err))
		return
	}
	defer projectResponse.Body.Close()

	if projectResponse.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Project not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	bodyResponse, err := io.ReadAll(projectResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading project resource response body", fmt.Sprintf("Error reading project resource response body: %s", err))
		return
	}

	if projectResponse.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error reading project", fmt.Sprintf("status: %v, body: %v", projectResponse.Status, string(bodyResponse)))
		return
	}

	project := &client.ProjectEntity{}

	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), project)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	state.Name = types.StringValue(project.Name)
	state.Description = types.StringPointerValue(project.Description)
	state.ID = types.StringValue(project.ID)

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Project Resource reading", map[string]any{"success": true})
}

func (r *ProjectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan ProjectResourceModel
	var state ProjectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.ProjectEntity{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueStringPointer(),
		ID:          state.ID.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	projectRequest, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/project/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating project resource request", fmt.Sprintf("Error creating project resource request: %s", err))
		return
	}
	projectRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	projectRequest.Header.Add("Content-Type", "application/vnd.api+json")

	projectResponse, err := r.client.Do(projectRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing project resource request", fmt.Sprintf("Error executing project resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(projectResponse.Body)
	projectResponse.Body.Close()
	if err != nil {
		resp.Diagnostics.AddError("Error reading project resource response body", fmt.Sprintf("Error reading project resource response body: %s", err))
		return
	}

	if projectResponse.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Project not found during update, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	if projectResponse.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error updating project", fmt.Sprintf("status: %v, body: %v", projectResponse.Status, string(bodyResponse)))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"success": string(bodyResponse)})

	projectRequest, err = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/project/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating project resource request", fmt.Sprintf("Error creating project resource request: %s", err))
		return
	}
	projectRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	projectRequest.Header.Add("Content-Type", "application/vnd.api+json")

	projectResponse, err = r.client.Do(projectRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing project resource request", fmt.Sprintf("Error executing project resource request: %s", err))
		return
	}
	defer projectResponse.Body.Close()

	bodyResponse, err = io.ReadAll(projectResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading project resource response body", fmt.Sprintf("Error reading project resource response body: %s", err))
		return
	}

	if projectResponse.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Project not found after update, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	if projectResponse.StatusCode >= http.StatusBadRequest {
		resp.Diagnostics.AddError("Error reading project after update", fmt.Sprintf("status: %v, body: %v", projectResponse.Status, string(bodyResponse)))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	project := &client.ProjectEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), project)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.Name = types.StringValue(project.Name)
	plan.Description = types.StringPointerValue(project.Description)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ProjectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ProjectResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	projectRequest, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/organization/%s/project/%s", r.endpoint, data.OrganizationId.ValueString(), data.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating project resource request", fmt.Sprintf("Error creating project resource request: %s", err))
		return
	}
	projectRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))

	delResp, err := r.client.Do(projectRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing project resource request", fmt.Sprintf("Error executing project resource request: %s", err))
		return
	}
	defer delResp.Body.Close()

	if delResp.StatusCode >= http.StatusBadRequest {
		bodyResponse, _ := io.ReadAll(delResp.Body)
		resp.Diagnostics.AddError("Error deleting project", fmt.Sprintf("status: %v, body: %v", delResp.Status, string(bodyResponse)))
		return
	}

	tflog.Info(ctx, "Project Resource deleted", map[string]any{"success": true})
}

func (r *ProjectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) != 2 || idParts[0] == "" || idParts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: 'organization_ID,ID', Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[1])...)
}
