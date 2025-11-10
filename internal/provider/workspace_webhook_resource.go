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
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &WorkspaceWebhookResource{}
var _ resource.ResourceWithImportState = &WorkspaceWebhookResource{}

type WorkspaceWebhookResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type WorkspaceWebhookResourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	WorkspaceId    types.String `tfsdk:"workspace_id"`
	Path           types.List   `tfsdk:"path"`
	Branch         types.List   `tfsdk:"branch"`
	TemplateId     types.String `tfsdk:"template_id"`
	RemoteHookId   types.String `tfsdk:"remote_hook_id"`
	Event          types.String `tfsdk:"event"`
}

func NewWorkspaceWebhookResource() resource.Resource {
	return &WorkspaceWebhookResource{}
}

func (r *WorkspaceWebhookResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_webhook"
}

func (r *WorkspaceWebhookResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a webhook attached to a workspace. Can be useful for automated apply/plan workflows.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Webhook ID",
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
			"path": schema.ListAttribute{
				Optional:    true,
				Description: "The file paths in regex that trigger a run.",
				ElementType: types.StringType,
			},
			"branch": schema.ListAttribute{
				Optional:    true,
				Description: "A list of branches that trigger a run. Support regex for more complex matching.",
				ElementType: types.StringType,
			},
			"template_id": schema.StringAttribute{
				Optional:    true,
				Description: "The template id to use for the run.",
			},
			"remote_hook_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "The remote hook ID.",
			},
			"event": schema.StringAttribute{
				Optional:    true,
				Description: "The event type that triggers a run. Supported values: `PUSH`, `PULL_REQUEST`, `RELEASE`.",
				Computed:    true,
				Default:     stringdefault.StaticString("PUSH"),
				Validators: []validator.String{
					stringvalidator.OneOf("PUSH", "PULL_REQUEST", "RELEASE"),
				},
			},
		},
	}
}

func (r *WorkspaceWebhookResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Workspace Webhook Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Webhook resource", map[string]any{"success": true})
}

func (r *WorkspaceWebhookResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceWebhookResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	var branchList, pathList []string
	plan.Branch.ElementsAs(ctx, &branchList, true)
	plan.Path.ElementsAs(ctx, &pathList, true)
	bodyRequest := &client.WorkspaceWebhookEntity{
		ID:         uuid.New().String(),
		Path:       strings.Join(pathList, ","),
		Branch:     strings.Join(branchList, ","),
		TemplateId: plan.TemplateId.ValueString(),
		Event:      plan.Event.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	request, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/webhook", r.endpoint, plan.OrganizationId.ValueString(), plan.WorkspaceId.ValueString()), strings.NewReader(out.String()))
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace webhook resource request", fmt.Sprintf("Error creating workspace webhook resource request %s", err))
		return
	}

	response, err := r.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace webhook resource request", fmt.Sprintf("Error executing workspace webhook resource request, response status %s, response body: %s, error: %s", response.Status, response.Body, err))
		return
	}

	bodyResponse, err := io.ReadAll(response.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading workspace webhook resource, response status %s, response body: %s, error: %s", response.Status, response.Body, err))
	}
	webhook := &client.WorkspaceWebhookEntity{}

	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), webhook)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: response status %s, response body: %s, error: %s", response.Status, response.Body, err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	plan.Path, _ = types.ListValueFrom(ctx, types.StringType, strings.Split(webhook.Path, ","))
	plan.Branch, _ = types.ListValueFrom(ctx, types.StringType, strings.Split(webhook.Branch, ","))
	plan.TemplateId = types.StringValue(webhook.TemplateId)
	plan.RemoteHookId = types.StringValue(webhook.RemoteHookId)
	plan.Event = types.StringValue(webhook.Event)
	plan.ID = types.StringValue(webhook.ID)

	tflog.Info(ctx, "Workspace Webhook Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceWebhookResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceWebhookResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	request, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/webhook/%s", r.endpoint, state.OrganizationId.ValueString(), state.WorkspaceId.ValueString(), state.ID.ValueString()), nil)
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace webhook resource request", fmt.Sprintf("Error creating workspace webhook resource request: %s", err))
		return
	}

	response, err := r.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace webhook resource request", fmt.Sprintf("Error executing workspace webhook resource request, response status %s, response body: %s, error: %s", response.Status, response.Body, err))
		return
	}

	bodyResponse, err := io.ReadAll(response.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading workspace webhook resource response, response status %s, response body: %s, error: %s", response.Status, response.Body, err))
	}
	webhook := &client.WorkspaceWebhookEntity{}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), webhook)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response, response status %s, response body: %s, error: %s", response.Status, response.Body, err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	state.Path, _ = types.ListValueFrom(ctx, types.StringType, strings.Split(webhook.Path, ","))
	state.Branch, _ = types.ListValueFrom(ctx, types.StringType, strings.Split(webhook.Branch, ","))
	state.TemplateId = types.StringValue(webhook.TemplateId)
	state.RemoteHookId = types.StringValue(webhook.RemoteHookId)
	state.Event = types.StringValue(webhook.Event)
	state.ID = types.StringValue(webhook.ID)

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Workspace Webhook Resource reading", map[string]any{"success": true})
}

func (r *WorkspaceWebhookResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan WorkspaceWebhookResourceModel
	var state WorkspaceWebhookResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var branchList, pathList []string
	plan.Branch.ElementsAs(ctx, &branchList, true)
	plan.Path.ElementsAs(ctx, &pathList, true)
	bodyRequest := &client.WorkspaceWebhookEntity{
		Path:         strings.Join(pathList, ","),
		Branch:       strings.Join(branchList, ","),
		TemplateId:   plan.TemplateId.ValueString(),
		RemoteHookId: state.RemoteHookId.ValueString(),
		Event:        plan.Event.ValueString(),
		ID:           state.ID.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	request, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/webhook/%s", r.endpoint, state.OrganizationId.ValueString(), state.WorkspaceId.ValueString(), state.ID.ValueString()), strings.NewReader(out.String()))
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating Workspace variable resource request", fmt.Sprintf("Error creating Workspace variable resource request: %s", err))
		return
	}

	response, err := r.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace webhook resource request", fmt.Sprintf("Error executing workspace webhook resource request, response status %s, response body: %s, error: %s", response.Status, response.Body, err))
		return
	}

	bodyResponse, err := io.ReadAll(response.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading Workspace webhook resource response, response status %s, response body: %s, error: %s", response.Status, response.Body, err))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"success": string(bodyResponse)})

	request, err = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/webhook/%s", r.endpoint, state.OrganizationId.ValueString(), state.WorkspaceId.ValueString(), state.ID.ValueString()), nil)
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace webhook resource request", fmt.Sprintf("Error creating workspace webhook resource request: %s", err))
		return
	}

	response, err = r.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace webhook resource request", fmt.Sprintf("Error executing workspace webhook resource request, response status %s, response body: %s, error: %s", response.Status, response.Body, err))
		return
	}

	bodyResponse, err = io.ReadAll(response.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading workspace webhook resource response body", fmt.Sprintf("Error reading workspace webhook resource response body, response status %s, response body: %s, error: %s", response.Status, response.Body, err))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	webhook := &client.WorkspaceWebhookEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), webhook)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.Path, _ = types.ListValueFrom(ctx, types.StringType, strings.Split(webhook.Path, ","))
	plan.Branch, _ = types.ListValueFrom(ctx, types.StringType, strings.Split(webhook.Branch, ","))
	plan.TemplateId = types.StringValue(webhook.TemplateId)
	plan.RemoteHookId = types.StringValue(webhook.RemoteHookId)
	plan.Event = types.StringValue(webhook.Event)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceWebhookResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data WorkspaceWebhookResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	request, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/webhook/%s", r.endpoint, data.OrganizationId.ValueString(), data.WorkspaceId.ValueString(), data.ID.ValueString()), nil)
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace webhook resource request", fmt.Sprintf("Error creating workspace webhook resource request: %s", err))
		return
	}

	response, err := r.client.Do(request)
	if err != nil || response.StatusCode != http.StatusNoContent {
		resp.Diagnostics.AddError("Error executing workspace webhook resource request", fmt.Sprintf("Error executing workspace webhook resource request, response status %s, response body: %s, error: %s", response.Status, response.Body, err))
		return
	}
}

func (r *WorkspaceWebhookResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
