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
var _ resource.Resource = &WorkspaceScheduleResource{}
var _ resource.ResourceWithImportState = &WorkspaceScheduleResource{}

type WorkspaceScheduleResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type WorkspaceScheduleResourceModel struct {
	ID          types.String `tfsdk:"id"`
	WorkspaceId types.String `tfsdk:"workspace_id"`
	TemplateId  types.String `tfsdk:"template_id"`
	Schedule    types.String `tfsdk:"schedule"`
}

func NewWorkspaceScheduleResource() resource.Resource {
	return &WorkspaceScheduleResource{}
}

func (r *WorkspaceScheduleResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_schedule"
}

func (r *WorkspaceScheduleResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a workspace schedule that will allow you to run templates on a regular basis.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Schedule Id",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"schedule": schema.StringAttribute{
				Required:    true,
				Description: "Schedule expression using java quartz notation",
			},
			"template_id": schema.StringAttribute{
				Required:    true,
				Description: "Template Id to be used when triggering a job",
			},
			"workspace_id": schema.StringAttribute{
				Required:    true,
				Description: "Workspace Id",
			},
		},
	}
}

func (r *WorkspaceScheduleResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Workspace Schedule Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Workspace Schedule resource", map[string]any{"success": true})
}

func (r *WorkspaceScheduleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceScheduleResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.WorkspaceScheduleEntity{
		Schedule:   plan.Schedule.ValueString(),
		TemplateId: plan.TemplateId.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	workspaceScheduleRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/workspace/%s/schedule", r.endpoint, plan.WorkspaceId.ValueString()), strings.NewReader(out.String()))
	workspaceScheduleRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceScheduleRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace schedule resource request", fmt.Sprintf("Error creating workspace schedule resource request: %s", err))
		return
	}

	workspaceScheduleResponse, err := r.client.Do(workspaceScheduleRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace schedule  resource request", fmt.Sprintf("Error executing workspace schedule  resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(workspaceScheduleResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading workspace schedule resource response")
	}
	workspaceSchedule := &client.WorkspaceScheduleEntity{}

	tflog.Info(ctx, "Body Response", map[string]any{"success": string(bodyResponse)})

	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), workspaceSchedule)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	plan.Schedule = types.StringValue(workspaceSchedule.Schedule)
	plan.TemplateId = types.StringValue(workspaceSchedule.TemplateId)
	plan.ID = types.StringValue(workspaceSchedule.ID)

	tflog.Info(ctx, "workspace schedule Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceScheduleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceScheduleResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceScheduleRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/workspace/%s/schedule/%s", r.endpoint, state.WorkspaceId.ValueString(), state.ID.ValueString()), nil)
	workspaceScheduleRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceScheduleRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace schedule resource request", fmt.Sprintf("Error creating workspace schedule resource request: %s", err))
		return
	}

	workspaceScheduleResponse, err := r.client.Do(workspaceScheduleRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace schedule resource request", fmt.Sprintf("Error executing workspace schedule resource request: %s", err))
		return
	}

	if workspaceScheduleResponse.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Workspace schedule not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	bodyResponse, err := io.ReadAll(workspaceScheduleResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading workspace schedule resource response")
	}
	workspaceSchedule := &client.WorkspaceScheduleEntity{}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), workspaceSchedule)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	state.Schedule = types.StringValue(workspaceSchedule.Schedule)
	state.TemplateId = types.StringValue(workspaceSchedule.TemplateId)
	state.ID = types.StringValue(workspaceSchedule.ID)

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Workspace schedule Resource reading", map[string]any{"success": true})
}

func (r *WorkspaceScheduleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan WorkspaceScheduleResourceModel
	var state WorkspaceScheduleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.WorkspaceScheduleEntity{
		Schedule:   plan.Schedule.ValueString(),
		TemplateId: plan.TemplateId.ValueString(),
		ID:         state.ID.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	workspaceScheduleReq, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/workspace/%s/schedule/%s", r.endpoint, state.WorkspaceId.ValueString(), state.ID.ValueString()), strings.NewReader(out.String()))
	workspaceScheduleReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceScheduleReq.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating Workspace schedule resource request", fmt.Sprintf("Error creating schedule schedule resource request: %s", err))
		return
	}

	workspaceScheduleResponse, err := r.client.Do(workspaceScheduleReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing Workspace schedule resource request", fmt.Sprintf("Error executing Workspace schedule resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(workspaceScheduleResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading Workspace schedule resource response")
	}

	tflog.Info(ctx, "Body Response", map[string]any{"success": string(bodyResponse)})

	workspaceScheduleReq, err = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/workspace/%s/schedule/%s", r.endpoint, state.WorkspaceId.ValueString(), state.ID.ValueString()), nil)
	workspaceScheduleReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceScheduleReq.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating Workspace schedule resource request", fmt.Sprintf("Error creating Workspace schedule resource request: %s", err))
		return
	}

	workspaceScheduleResponse, err = r.client.Do(workspaceScheduleReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing Workspace schedule resource request", fmt.Sprintf("Error executing Workspace schedule resource request: %s", err))
		return
	}

	bodyResponse, err = io.ReadAll(workspaceScheduleResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading Workspace schedule resource response body", fmt.Sprintf("Error reading Workspace schedule resource response body: %s", err))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	workspaceSchedule := &client.WorkspaceScheduleEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), workspaceSchedule)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.Schedule = types.StringValue(workspaceSchedule.Schedule)
	plan.TemplateId = types.StringValue(workspaceSchedule.TemplateId)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceScheduleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data WorkspaceScheduleResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	workspaceRequest, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/workspace/%s/schedule/%s", r.endpoint, data.WorkspaceId.ValueString(), data.ID.ValueString()), nil)
	workspaceRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	if err != nil {
		resp.Diagnostics.AddError("Error creating Workspace schedule resource request", fmt.Sprintf("Error creating schedule schedule resource request: %s", err))
		return
	}

	_, err = r.client.Do(workspaceRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing Workspace schedule resource request", fmt.Sprintf("Error executing Workspace schedule resource request: %s", err))
		return
	}
}

func (r *WorkspaceScheduleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) != 3 || idParts[0] == "" || idParts[1] == "" || idParts[2] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: 'organization_ID,workspace_ID, ID', Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), idParts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[2])...)
}
