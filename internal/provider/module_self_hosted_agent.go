package provider

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"github.com/google/jsonapi"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"io"
	"net/http"
	"strings"
	"terraform-provider-terrakube/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &AgentResource{}
var _ resource.ResourceWithImportState = &AgentResource{}

type AgentResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type AgentResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Description    types.String `tfsdk:"description"`
	Url            types.String `tfsdk:"url"`
}

func NewAgentResource() resource.Resource {
	return &AgentResource{}
}

func (r *AgentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_self_hosted_agent"
}

func (r *AgentResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Resource for managing self hosted agents in Terrakube. " +
			"This resource allows you to create, read, update, and delete self hosted agents within a specified organization.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Agent Id",
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
				Description: "Self hosted agent name",
			},
			"description": schema.StringAttribute{
				Required:    true,
				Description: "Description of the self hosted agent",
			},
			"url": schema.StringAttribute{
				Required:    true,
				Description: "Url of the self hosted agent",
			},
		},
	}
}

func (r *AgentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Self Hosted Agent Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Self Hosted Agent resource", map[string]any{"success": true})
}

func (r *AgentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan AgentResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.AgentEntity{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
		Url:         plan.Url.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	tflog.Info(ctx, fmt.Sprintf("Body Request: %s", out.String()))

	agentRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/organization/%s/agent", r.endpoint, plan.OrganizationId.ValueString()), strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating self hosted agent resource request", fmt.Sprintf("Error creating self hosted agent resource request: %s", err))
		return
	}
	agentRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	agentRequest.Header.Add("Content-Type", "application/vnd.api+json")

	agentResponse, err := r.client.Do(agentRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing self hosted agent resource request", fmt.Sprintf("Error executing self hosted agent resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(agentResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading self hosted agent resource response")
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	newAgent := &client.AgentEntity{}

	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), newAgent)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	plan.ID = types.StringValue(newAgent.ID)
	plan.Name = types.StringValue(newAgent.Name)
	plan.Description = types.StringValue(newAgent.Description)
	plan.Url = types.StringValue(newAgent.Url)

	tflog.Info(ctx, "Module Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *AgentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state AgentResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	agentRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/agent/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating self hosted agent resource request", fmt.Sprintf("Error creating self hosted agent resource request: %s", err))
		return
	}
	agentRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	agentRequest.Header.Add("Content-Type", "application/vnd.api+json")

	agentResponse, err := r.client.Do(agentRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing self hosted agent resource request", fmt.Sprintf("Error executing self hosted agent resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(agentResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading self hosted agent resource response")
	}
	agent := &client.AgentEntity{}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), agent)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	state.Name = types.StringValue(agent.Name)
	state.Description = types.StringValue(agent.Description)
	state.Url = types.StringValue(agent.Url)

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Self Hosted Agent Resource reading", map[string]any{"success": true})
}

func (r *AgentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan AgentResourceModel
	var state AgentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.AgentEntity{
		ID:          state.ID.ValueString(),
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
		Url:         plan.Url.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	agentRequest, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/agent/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating self hosted agent resource request", fmt.Sprintf("Error creating self hosted agent resource request: %s", err))
		return
	}
	agentRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	agentRequest.Header.Add("Content-Type", "application/vnd.api+json")

	agentResponse, err := r.client.Do(agentRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing self hosted agent resource request", fmt.Sprintf("Error executing self hosted agent resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(agentResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading self hosted agent resource response")
	}

	tflog.Info(ctx, "Body Response", map[string]any{"success": string(bodyResponse)})

	agentRequest, err = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/agent/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating self hosted agent resource request", fmt.Sprintf("Error creating self hosted agent resource request: %s", err))
		return
	}
	agentRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	agentRequest.Header.Add("Content-Type", "application/vnd.api+json")

	agentResponse, err = r.client.Do(agentRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing self hosted agent resource request", fmt.Sprintf("Error executing self hosted agent resource request: %s", err))
		return
	}

	bodyResponse, err = io.ReadAll(agentResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading self hosted agent resource response body", fmt.Sprintf("Error reading self hosted agent resource response body: %s", err))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	module := &client.AgentEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), module)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.Name = types.StringValue(module.Name)
	plan.Description = types.StringValue(module.Description)
	plan.Url = types.StringValue(module.Url)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *AgentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data AgentResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	reqOrg, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/organization/%s/agent/%s", r.endpoint, data.OrganizationId.ValueString(), data.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating self hosted agent resource request", fmt.Sprintf("Error creating self hosted agent resource request: %s", err))
		return
	}
	reqOrg.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))

	_, err = r.client.Do(reqOrg)
	if err != nil {
		resp.Diagnostics.AddError("Error executing self hosted agent resource request", fmt.Sprintf("Error executing self hosted agent resource request: %s", err))
		return
	}
}

func (r *AgentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
