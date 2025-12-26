package provider

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"terraform-provider-terrakube/internal/client"

	"github.com/google/jsonapi"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &WorkspaceCliResource{}
var _ resource.ResourceWithImportState = &WorkspaceCliResource{}

type WorkspaceCliResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type WorkspaceCliResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Description    types.String `tfsdk:"description"`
	IaCType        types.String `tfsdk:"iac_type"`
	IaCVersion     types.String `tfsdk:"iac_version"`
	ExecutionMode  types.String `tfsdk:"execution_mode"`
}

func NewWorkspaceCliResource() resource.Resource {
	return &WorkspaceCliResource{}
}

func (r *WorkspaceCliResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_cli"
}

func (r *WorkspaceCliResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a CLI workspace for Terrakube. When running plan from UI with CLI workspace " +
			"only the current state will be compared to the cloud provider API not taking into account the file contained" +
			"in workspace working directory. If you want to fetch files from github use vcs_workspace instead.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Workspace CLI Id",
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
				Description: "Workspace CLI name",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Workspace CLI description",
			},
			"execution_mode": schema.StringAttribute{
				Required:    true,
				Description: "Workspace CLI execution mode (remote or local). Remote execution will require setting up executor.",
			},
			"iac_type": schema.StringAttribute{
				Required:    true,
				Description: "Workspace CLI IaC type (Supported values terraform or tofu)",
			},
			"iac_version": schema.StringAttribute{
				Required:    true,
				Description: "Workspace CLI IaC type",
			},
		},
	}
}

func (r *WorkspaceCliResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Team Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Workspace CLI resource", map[string]any{"success": true})
}

func (r *WorkspaceCliResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceCliResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.WorkspaceEntity{
		Name:          plan.Name.ValueString(),
		Description:   plan.Description.ValueStringPointer(),
		Source:        "empty",
		Branch:        "remote-content",
		IaCType:       plan.IaCType.ValueString(),
		IaCVersion:    plan.IaCVersion.ValueString(),
		ExecutionMode: plan.ExecutionMode.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	workspaceCliRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/organization/%s/workspace", r.endpoint, plan.OrganizationId.ValueString()), strings.NewReader(out.String()))
	workspaceCliRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceCliRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace cli resource request", fmt.Sprintf("Error creating workspace cli resource request: %s", err))
		return
	}

	workspaceCliResponse, err := r.client.Do(workspaceCliRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace cli resource request", fmt.Sprintf("Error executing workspace cli resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(workspaceCliResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading workspace cli resource response")
	}
	newWorkspaceCli := &client.WorkspaceEntity{}

	fmt.Println(string(bodyResponse))
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), newWorkspaceCli)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	plan.ID = types.StringValue(newWorkspaceCli.ID)
	plan.Name = types.StringValue(newWorkspaceCli.Name)
	plan.Description = types.StringPointerValue(newWorkspaceCli.Description)
	plan.IaCType = types.StringValue(newWorkspaceCli.IaCType)
	plan.IaCVersion = types.StringValue(newWorkspaceCli.IaCVersion)
	plan.ExecutionMode = types.StringValue(newWorkspaceCli.ExecutionMode)

	tflog.Info(ctx, "Workspace Cli Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceCliResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceCliResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	workspaceRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace cli resource request", fmt.Sprintf("Error creating workspace cli resource request: %s", err))
		return
	}

	workspaceResponse, err := r.client.Do(workspaceRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace cli resource request", fmt.Sprintf("Error executing workspace cli resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(workspaceResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading workspace cli resource response")
	}
	workspace := &client.WorkspaceEntity{}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), workspace)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	state.Name = types.StringValue(workspace.Name)
	state.Description = types.StringPointerValue(workspace.Description)
	state.ExecutionMode = types.StringValue(workspace.ExecutionMode)
	state.IaCType = types.StringValue(workspace.IaCType)
	state.IaCVersion = types.StringValue(workspace.IaCVersion)
	state.ID = types.StringValue(workspace.ID)

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Workspace Cli Resource reading", map[string]any{"success": true})
}

func (r *WorkspaceCliResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan WorkspaceCliResourceModel
	var state WorkspaceCliResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.WorkspaceEntity{
		IaCVersion:    plan.IaCVersion.ValueString(),
		IaCType:       plan.IaCType.ValueString(),
		ExecutionMode: plan.ExecutionMode.ValueString(),
		Description:   plan.Description.ValueStringPointer(),
		Source:        "empty",
		Branch:        "remote-content",
		Name:          plan.Name.ValueString(),
		ID:            state.ID.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	organizationRequest, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), strings.NewReader(out.String()))
	organizationRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	organizationRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace cli resource request", fmt.Sprintf("Error creating workspace cli resource request: %s", err))
		return
	}

	organizationResponse, err := r.client.Do(organizationRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace cli resource request", fmt.Sprintf("Error executing workspace cli resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(organizationResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading workspace cli resource response")
	}

	tflog.Info(ctx, "Body Response", map[string]any{"success": string(bodyResponse)})

	organizationRequest, err = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	organizationRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	organizationRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace cli resource request", fmt.Sprintf("Error creating workspace cli resource request: %s", err))
		return
	}

	organizationResponse, err = r.client.Do(organizationRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace cli resource request", fmt.Sprintf("Error executing workspace cli resource request: %s", err))
		return
	}

	bodyResponse, err = io.ReadAll(organizationResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading workspace cli resource response body", fmt.Sprintf("Error reading workspace cli resource response body: %s", err))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	workspace := &client.WorkspaceEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), workspace)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.Name = types.StringValue(workspace.Name)
	plan.Description = types.StringPointerValue(workspace.Description)
	plan.IaCType = types.StringValue(workspace.IaCType)
	plan.IaCVersion = types.StringValue(workspace.IaCVersion)
	plan.ExecutionMode = types.StringValue(workspace.ExecutionMode)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceCliResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data WorkspaceCliResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	var chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890"

	ll := len(chars)
	b := make([]byte, 4)

	if _, err := rand.Read(b); err != nil {
		resp.Diagnostics.AddError("Error generating random string to delete workspace", fmt.Sprintf("Error generating random string to delete workspace: %s", err))
		return
	}

	for i := 0; i < 4; i++ {
		b[i] = chars[int(b[i])%ll]
	}

	tflog.Info(ctx, "Send patch request to mark as deleted...")
	tflog.Info(ctx, fmt.Sprintf("%s_DEL_%s", data.Name.ValueString(), string(b)))

	bodyRequest := &client.WorkspaceEntity{
		ID:            data.ID.ValueString(),
		Name:          fmt.Sprintf("%s_DEL_%s", data.Name.ValueString(), string(b)), // FORCE A NAME CHANGE WITH THE SAME LOGIC THAT IN THE UI
		Description:   data.Description.ValueStringPointer(),
		Source:        "empty",
		Branch:        "remote-content",
		IaCType:       data.IaCType.ValueString(),
		IaCVersion:    data.IaCVersion.ValueString(),
		ExecutionMode: data.ExecutionMode.ValueString(),
		Deleted:       true,
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	tflog.Info(ctx, "Request Body...")
	tflog.Info(ctx, out.String())

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	workspaceCliRequest, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s", r.endpoint, data.OrganizationId.ValueString(), data.ID.ValueString()), strings.NewReader(out.String()))
	workspaceCliRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceCliRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating cli resource request", fmt.Sprintf("Error creating cli resource request: %s", err))
		return
	}

	workspaceCliResponse, err := r.client.Do(workspaceCliRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing cli resource request", fmt.Sprintf("Error executing cli resource request: %s", err))
		return
	}

	tflog.Info(ctx, "Delete response code: "+strconv.Itoa(workspaceCliResponse.StatusCode))

}

func (r *WorkspaceCliResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
