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

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &WorkspaceVcsResource{}
var _ resource.ResourceWithImportState = &WorkspaceVcsResource{}

type WorkspaceVcsResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type WorkspaceVcsResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	OrganizationId   types.String `tfsdk:"organization_id"`
	Description      types.String `tfsdk:"description"`
	IaCType          types.String `tfsdk:"iac_type"`
	TemplateId       types.String `tfsdk:"template_id"`
	IaCVersion       types.String `tfsdk:"iac_version"`
	Repository       types.String `tfsdk:"repository"`
	Branch           types.String `tfsdk:"branch"`
	Folder           types.String `tfsdk:"folder"`
	ExecutionMode    types.String `tfsdk:"execution_mode"`
	VcsId            types.String `tfsdk:"vcs_id"`
	AllowRemoteApply types.Bool   `tfsdk:"allow_remote_apply"`
}

func NewWorkspaceVcsResource() resource.Resource {
	return &WorkspaceVcsResource{}
}

func (r *WorkspaceVcsResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_vcs"
}

func (r *WorkspaceVcsResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a VCS workspace for Terrakube. When running plan from UI with VCS workspace " +
			"it will compare files contained in github repository with the cloud provider. If you only want to compare state " +
			"with cloud provider API use CLI workspace instead.",

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
				Description: "Workspace VCS name",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Workspace VCS description",
			},
			"execution_mode": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("remote"),
				Description: "Workspace VCS execution mode (remote or local)",
				Validators: []validator.String{
					stringvalidator.OneOf("remote", "local"),
				},
			},
			"iac_type": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("terraform"),
				Description: "Workspace VCS IaC type (Supported values terraform or tofu)",
				Validators: []validator.String{
					stringvalidator.OneOf("terraform", "tofu"),
				},
			},
			"iac_version": schema.StringAttribute{
				Required:    true,
				Description: "Workspace VCS VCS type",
			},
			"repository": schema.StringAttribute{
				Required:    true,
				Description: "Workspace VCS repository",
			},
			"template_id": schema.StringAttribute{
				Required:    true,
				Description: "Default template ID for the workspace",
			},
			"branch": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("main"),
				Description: "Workspace VCS branch",
			},
			"folder": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("/"),
				Description: "Workspace VCS folder",
			},
			"vcs_id": schema.StringAttribute{
				Optional:    true,
				Description: "VCS connection ID for private workspaces",
			},
			"allow_remote_apply": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Wether to allow remote apply. By default false to respect VCS philosophy.",
			},
		},
	}
}

func (r *WorkspaceVcsResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

	tflog.Debug(ctx, "Configuring Workspace VCS resource", map[string]any{"success": true})
}

func (r *WorkspaceVcsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceVcsResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.WorkspaceEntity{
		Name:             plan.Name.ValueString(),
		Description:      plan.Description.ValueStringPointer(),
		Source:           plan.Repository.ValueString(),
		Branch:           plan.Branch.ValueString(),
		IaCType:          plan.IaCType.ValueString(),
		IaCVersion:       plan.IaCVersion.ValueString(),
		Folder:           plan.Folder.ValueString(),
		TemplateId:       plan.TemplateId.ValueString(),
		ExecutionMode:    plan.ExecutionMode.ValueString(),
		AllowRemoteApply: plan.AllowRemoteApply.ValueBool(),
	}

	if !plan.VcsId.IsNull() {
		tflog.Info(ctx, fmt.Sprintf("Workspace using Vcs connection id: %s", plan.VcsId.ValueString()))
		bodyRequest.Vcs = &client.VcsEntity{ID: plan.VcsId.ValueString()}
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	workspaceVcsRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/organization/%s/workspace", r.endpoint, plan.OrganizationId.ValueString()), strings.NewReader(out.String()))
	workspaceVcsRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceVcsRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace vcs resource request", fmt.Sprintf("Error creating workspace vcs resource request: %s", err))
		return
	}

	workspaceVcsResponse, err := r.client.Do(workspaceVcsRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace vcs resource request", fmt.Sprintf("Error executing workspace vcs resource request, response status: %s, response body: %s, error: %s", workspaceVcsResponse.Status, workspaceVcsResponse.Body, err))
		return
	}

	bodyResponse, err := io.ReadAll(workspaceVcsResponse.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading workspace vcs resource response, response status: %s, response body: %s, error: %s", workspaceVcsResponse.Status, workspaceVcsResponse.Body, err))
	}
	newWorkspaceVcs := &client.WorkspaceEntity{}

	fmt.Println(string(bodyResponse))
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), newWorkspaceVcs)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response, response status: %s, response body: %s, error: %s", workspaceVcsResponse.Status, workspaceVcsResponse.Body, err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	plan.ID = types.StringValue(newWorkspaceVcs.ID)
	plan.Name = types.StringValue(newWorkspaceVcs.Name)
	plan.Description = types.StringPointerValue(newWorkspaceVcs.Description)
	plan.Repository = types.StringValue(newWorkspaceVcs.Source)
	plan.Branch = types.StringValue(newWorkspaceVcs.Branch)
	plan.IaCType = types.StringValue(newWorkspaceVcs.IaCType)
	plan.IaCVersion = types.StringValue(newWorkspaceVcs.IaCVersion)

	plan.TemplateId = types.StringValue(newWorkspaceVcs.TemplateId)
	plan.ExecutionMode = types.StringValue(newWorkspaceVcs.ExecutionMode)
	plan.AllowRemoteApply = types.BoolValue(newWorkspaceVcs.AllowRemoteApply)

	if !plan.VcsId.IsNull() {
		plan.VcsId = types.StringValue(newWorkspaceVcs.Vcs.ID)
	}

	if !plan.Folder.IsNull() {
		plan.Folder = types.StringValue(newWorkspaceVcs.Folder)
	}

	tflog.Info(ctx, "Workspace VCS Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceVcsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceVcsResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	workspaceRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace vcs resource request", fmt.Sprintf("Error creating workspace cli resource request: %s", err))
		return
	}

	workspaceResponse, err := r.client.Do(workspaceRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace vcs resource request", fmt.Sprintf("Error executing workspace cli resource request, response status: %s, response body: %s, error: %s", workspaceResponse.Status, workspaceResponse.Body, err))
		return
	}

	bodyResponse, err := io.ReadAll(workspaceResponse.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading workspace vcs resource response, response status: %s, response body: %s, error: %s", workspaceResponse.Status, workspaceResponse.Body, err))
	}
	workspace := &client.WorkspaceEntity{}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), workspace)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response, response status: %s, response body: %s, error: %s", workspaceResponse.Status, workspaceResponse.Body, err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	state.Name = types.StringValue(workspace.Name)
	state.Description = types.StringPointerValue(workspace.Description)
	state.ExecutionMode = types.StringValue(workspace.ExecutionMode)
	state.Repository = types.StringValue(workspace.Source)
	state.Branch = types.StringValue(workspace.Branch)
	state.IaCType = types.StringValue(workspace.IaCType)
	state.Folder = types.StringValue(workspace.Folder)
	state.TemplateId = types.StringValue(workspace.TemplateId)
	state.IaCVersion = types.StringValue(workspace.IaCVersion)
	state.ID = types.StringValue(workspace.ID)
	state.AllowRemoteApply = types.BoolValue(workspace.AllowRemoteApply)

	if workspace.Vcs != nil {
		state.VcsId = types.StringValue(workspace.Vcs.ID)
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Workspace vcs Resource reading", map[string]any{"success": true})
}

func (r *WorkspaceVcsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan WorkspaceVcsResourceModel
	var state WorkspaceVcsResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.WorkspaceEntity{
		IaCVersion:       plan.IaCVersion.ValueString(),
		IaCType:          plan.IaCType.ValueString(),
		ExecutionMode:    plan.ExecutionMode.ValueString(),
		Description:      plan.Description.ValueStringPointer(),
		Source:           plan.Repository.ValueString(),
		Branch:           plan.Branch.ValueString(),
		Folder:           plan.Folder.ValueString(),
		TemplateId:       plan.TemplateId.ValueString(),
		Name:             plan.Name.ValueString(),
		ID:               state.ID.ValueString(),
		AllowRemoteApply: plan.AllowRemoteApply.ValueBool(),
	}

	if !plan.VcsId.IsNull() {
		tflog.Info(ctx, fmt.Sprintf("Workspace using Vcs connection id: %s", plan.VcsId.ValueString()))
		bodyRequest.Vcs = &client.VcsEntity{ID: plan.VcsId.ValueString()}
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
		resp.Diagnostics.AddError("Error creating workspace vcs resource request", fmt.Sprintf("Error creating workspace vcs resource request: %s", err))
		return
	}

	organizationResponse, err := r.client.Do(organizationRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace vcs resource request", fmt.Sprintf("Error executing workspace vcs resource request, response status: %s, response body: %s, error: %s", organizationResponse.Status, organizationResponse.Body, err))
		return
	}

	bodyResponse, err := io.ReadAll(organizationResponse.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading workspace vcs resource response, response status: %s, response body: %s, error: %s", organizationResponse.Status, organizationResponse.Body, err))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"success": string(bodyResponse)})

	organizationRequest, err = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	organizationRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	organizationRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace vcs resource request", fmt.Sprintf("Error creating workspace vcs resource request: %s", err))
		return
	}

	organizationResponse, err = r.client.Do(organizationRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace vcs resource request", fmt.Sprintf("Error executing workspace vcs resource request, response status: %s, response body: %s, error: %s", organizationResponse.Status, organizationResponse.Body, err))
		return
	}

	bodyResponse, err = io.ReadAll(organizationResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading workspace vcs resource response body", fmt.Sprintf("Error reading workspace vcs resource response body, response status: %s, response body: %s, error: %s", organizationResponse.Status, organizationResponse.Body, err))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	workspace := &client.WorkspaceEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), workspace)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response, response status: %s, response body: %s, error: %s", organizationResponse.Status, organizationResponse.Body, err))
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.Name = types.StringValue(workspace.Name)
	plan.Description = types.StringPointerValue(workspace.Description)
	plan.Repository = types.StringValue(workspace.Source)
	plan.Branch = types.StringValue(workspace.Branch)
	plan.IaCType = types.StringValue(workspace.IaCType)
	plan.IaCVersion = types.StringValue(workspace.IaCVersion)
	plan.ExecutionMode = types.StringValue(workspace.ExecutionMode)
	plan.Folder = types.StringValue(workspace.Folder)
	plan.TemplateId = types.StringValue(workspace.TemplateId)
	plan.AllowRemoteApply = types.BoolValue(workspace.AllowRemoteApply)
	if workspace.Vcs != nil {
		plan.VcsId = types.StringValue(workspace.Vcs.ID)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceVcsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data WorkspaceVcsResourceModel

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
		ID:               data.ID.ValueString(),
		Name:             fmt.Sprintf("%s_DEL_%s", data.Name.ValueString(), string(b)), // FORCE A NAME CHANGE WITH THE SAME LOGIC THAT IN THE UI
		Description:      data.Description.ValueStringPointer(),
		Source:           data.Repository.ValueString(),
		Branch:           data.Branch.ValueString(),
		IaCType:          data.IaCType.ValueString(),
		TemplateId:       data.TemplateId.ValueString(),
		IaCVersion:       data.IaCVersion.ValueString(),
		ExecutionMode:    data.ExecutionMode.ValueString(),
		AllowRemoteApply: data.AllowRemoteApply.ValueBool(),
		Deleted:          true,
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	tflog.Info(ctx, "Request Body...")
	tflog.Info(ctx, out.String())

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	workspaceVcsRequest, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s", r.endpoint, data.OrganizationId.ValueString(), data.ID.ValueString()), strings.NewReader(out.String()))
	workspaceVcsRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceVcsRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating vcs resource request", fmt.Sprintf("Error creating vcs resource request: %s", err))
		return
	}

	workspaceVcsResponse, err := r.client.Do(workspaceVcsRequest)
	if err != nil || workspaceVcsResponse.StatusCode != http.StatusNoContent {
		resp.Diagnostics.AddError("Error executing vcs resource request", fmt.Sprintf("Error executing vcs resource request, response status: %s, response body: %s, error: %s", workspaceVcsResponse.Status, workspaceVcsResponse.Body, err))
		return
	}

	tflog.Info(ctx, "Delete response code: "+strconv.Itoa(workspaceVcsResponse.StatusCode))
}

func (r *WorkspaceVcsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
