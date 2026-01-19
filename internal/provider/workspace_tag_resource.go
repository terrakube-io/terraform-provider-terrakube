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

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &WorkspaceTagResource{}
var _ resource.ResourceWithImportState = &WorkspaceTagResource{}

type WorkspaceTagResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type WorkspaceTagResourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	WorkspaceId    types.String `tfsdk:"workspace_id"`
	TagID          types.String `tfsdk:"tag_id"`
}

func NewWorkspaceTagResource() resource.Resource {
	return &WorkspaceTagResource{}
}

func (r *WorkspaceTagResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_tag"
}

func (r *WorkspaceTagResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Adds a tag to a workspace resource.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Workspace Tag Id",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"tag_id": schema.StringAttribute{
				Required:    true,
				Description: "Tag Id",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization id",
			},
			"workspace_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube workspace id",
			},
		},
	}
}

func (r *WorkspaceTagResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Workspace Tag Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Workspace Tag resource", map[string]any{"success": true})
}

func (r *WorkspaceTagResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceTagResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.WorkspaceTagEntity{
		TagID: plan.TagID.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	workspaceTagRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/workspaceTag", r.endpoint, plan.OrganizationId.ValueString(), plan.WorkspaceId.ValueString()), strings.NewReader(out.String()))
	workspaceTagRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceTagRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace tag resource request", fmt.Sprintf("Error creating workspace tag resource request: %s", err))
		return
	}

	workspaceTagResponse, err := r.client.Do(workspaceTagRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace tag resource request", fmt.Sprintf("Error executing workspace tag resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(workspaceTagResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading workspace tag resource response")
	}
	newWorkspaceTag := &client.WorkspaceTagEntity{}

	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), newWorkspaceTag)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	plan.ID = types.StringValue(newWorkspaceTag.ID)
	plan.TagID = types.StringValue(newWorkspaceTag.TagID)

	tflog.Info(ctx, "Workspace Tag Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceTagResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	tflog.Warn(ctx, "Workspace Tag Resource doesn't have an update action", map[string]any{"success": true})
}

func (r *WorkspaceTagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceTagResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceTagRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/workspaceTag/%s", r.endpoint, state.OrganizationId.ValueString(), state.WorkspaceId.ValueString(), state.ID.ValueString()), nil)
	workspaceTagRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceTagRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace tag resource request", fmt.Sprintf("Error creating workspace tag resource request: %s", err))
		return
	}

	workspaceTagResponse, err := r.client.Do(workspaceTagRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace tag resource request", fmt.Sprintf("Error executing workspace workspace tag resource request: %s", err))
		return
	}

	if workspaceTagResponse.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Workspace tag not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	bodyResponse, err := io.ReadAll(workspaceTagResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading workspace tag resource response")
	}
	workspaceTag := &client.WorkspaceTagEntity{}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), workspaceTag)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	state.ID = types.StringValue(workspaceTag.ID)
	state.TagID = types.StringValue(workspaceTag.TagID)

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Workspace tag Resource reading", map[string]any{"success": true})
}

func (r *WorkspaceTagResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data WorkspaceTagResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	reqOrg, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/workspaceTag/%s", r.endpoint, data.OrganizationId.ValueString(), data.WorkspaceId.ValueString(), data.TagID.ValueString()), nil)
	reqOrg.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace tag resource request", fmt.Sprintf("Error creating workspace tag resource request: %s", err))
		return
	}

	_, err = r.client.Do(reqOrg)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace tag resource request", fmt.Sprintf("Error executing workspace tag resource request: %s", err))
		return
	}
}

func (r *WorkspaceTagResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.AddError("Import not implemented", "Import is not implemented for Workspace Tag Resource, please delete and recreate the resource")
}
