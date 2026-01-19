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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &TeamResource{}
var _ resource.ResourceWithImportState = &TeamResource{}

type TeamResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type TeamResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	OrganizationId   types.String `tfsdk:"organization_id"`
	ManageState      types.Bool   `tfsdk:"manage_state"`
	ManageWorkspace  types.Bool   `tfsdk:"manage_workspace"`
	ManageModule     types.Bool   `tfsdk:"manage_module"`
	ManageProvider   types.Bool   `tfsdk:"manage_provider"`
	ManageVcs        types.Bool   `tfsdk:"manage_vcs"`
	ManageTemplate   types.Bool   `tfsdk:"manage_template"`
	ManageJob        types.Bool   `tfsdk:"manage_job"`
	ManageCollection types.Bool   `tfsdk:"manage_collection"`
}

func NewTeamResource() resource.Resource {
	return &TeamResource{}
}

func (r *TeamResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_team"
}

func (r *TeamResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a team and bind it to an organization. Allows for fined grained access management.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Team Id",
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
				Description: "Allow to manage and trigger jobs",
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"manage_collection": schema.BoolAttribute{
				Optional:    true,
				Description: "Allow to manage variables collection",
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"manage_workspace": schema.BoolAttribute{
				Optional:    true,
				Description: "Allow to manage workspaces",
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"manage_module": schema.BoolAttribute{
				Optional:    true,
				Description: "Allow to manage modules",
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"manage_provider": schema.BoolAttribute{
				Optional:    true,
				Description: "Allow to manage providers",
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"manage_vcs": schema.BoolAttribute{
				Optional:    true,
				Description: "Allow to manage vcs connections",
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"manage_template": schema.BoolAttribute{
				Optional:    true,
				Description: "Allow to manage templates",
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
		},
	}
}

func (r *TeamResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

	tflog.Debug(ctx, "Configuring Team resource", map[string]any{"success": true})
}

func (r *TeamResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan TeamResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.TeamEntity{
		Name:             plan.Name.ValueString(),
		ManageState:      plan.ManageState.ValueBool(),
		ManageWorkspace:  plan.ManageWorkspace.ValueBool(),
		ManageModule:     plan.ManageModule.ValueBool(),
		ManageProvider:   plan.ManageProvider.ValueBool(),
		ManageTemplate:   plan.ManageTemplate.ValueBool(),
		ManageVcs:        plan.ManageVcs.ValueBool(),
		ManageJob:        plan.ManageJob.ValueBool(),
		ManageCollection: plan.ManageCollection.ValueBool(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	teamRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/organization/%s/team", r.endpoint, plan.OrganizationId.ValueString()), strings.NewReader(out.String()))
	teamRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	teamRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating team resource request", fmt.Sprintf("Error creating team resource request: %s", err))
		return
	}

	teamResponse, err := r.client.Do(teamRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing team resource request", fmt.Sprintf("Error executing team resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(teamResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading team resource response")
	}
	newTeam := &client.TeamEntity{}

	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), newTeam)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	plan.ID = types.StringValue(newTeam.ID)
	plan.Name = types.StringValue(newTeam.Name)
	plan.ManageState = types.BoolValue(newTeam.ManageState)
	plan.ManageWorkspace = types.BoolValue(newTeam.ManageWorkspace)
	plan.ManageModule = types.BoolValue(newTeam.ManageModule)
	plan.ManageVcs = types.BoolValue(newTeam.ManageVcs)
	plan.ManageProvider = types.BoolValue(newTeam.ManageProvider)
	plan.ManageTemplate = types.BoolValue(newTeam.ManageTemplate)
	plan.ManageJob = types.BoolValue(newTeam.ManageJob)
	plan.ManageCollection = types.BoolValue(newTeam.ManageCollection)

	tflog.Info(ctx, "Team Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *TeamResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state TeamResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	teamRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/team/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	teamRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	teamRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating team resource request", fmt.Sprintf("Error creating team resource request: %s", err))
		return
	}

	teamResponse, err := r.client.Do(teamRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing team resource request", fmt.Sprintf("Error executing team resource request: %s", err))
		return
	}

	if teamResponse.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Team not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	bodyResponse, err := io.ReadAll(teamResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading team resource response")
	}
	team := &client.TeamEntity{}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), team)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	state.Name = types.StringValue(team.Name)
	state.ManageState = types.BoolValue(team.ManageState)
	state.ManageWorkspace = types.BoolValue(team.ManageWorkspace)
	state.ManageModule = types.BoolValue(team.ManageModule)
	state.ManageVcs = types.BoolValue(team.ManageVcs)
	state.ManageProvider = types.BoolValue(team.ManageProvider)
	state.ManageTemplate = types.BoolValue(team.ManageTemplate)
	state.ManageJob = types.BoolValue(team.ManageJob)
	state.ManageCollection = types.BoolValue(team.ManageCollection)

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Team Resource reading", map[string]any{"success": true})
}

func (r *TeamResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan TeamResourceModel
	var state TeamResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.TeamEntity{
		ManageState:      plan.ManageState.ValueBool(),
		ManageWorkspace:  plan.ManageWorkspace.ValueBool(),
		ManageModule:     plan.ManageModule.ValueBool(),
		ManageProvider:   plan.ManageProvider.ValueBool(),
		ManageTemplate:   plan.ManageTemplate.ValueBool(),
		ManageVcs:        plan.ManageVcs.ValueBool(),
		ManageJob:        plan.ManageJob.ValueBool(),
		ManageCollection: plan.ManageCollection.ValueBool(),
		ID:               state.ID.ValueString(),
		Name:             state.Name.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	teamRequest, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/team/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), strings.NewReader(out.String()))
	teamRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	teamRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating team resource request", fmt.Sprintf("Error creating team resource request: %s", err))
		return
	}

	teamResponse, err := r.client.Do(teamRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing team resource request", fmt.Sprintf("Error executing team resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(teamResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading team resource response")
	}

	tflog.Info(ctx, "Body Response", map[string]any{"success": string(bodyResponse)})

	teamRequest, err = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/team/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	teamRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	teamRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating team resource request", fmt.Sprintf("Error creating team resource request: %s", err))
		return
	}

	teamResponse, err = r.client.Do(teamRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing team resource request", fmt.Sprintf("Error executing team resource request: %s", err))
		return
	}

	bodyResponse, err = io.ReadAll(teamResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading team resource response body", fmt.Sprintf("Error reading team resource response body: %s", err))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	team := &client.TeamEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), team)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.Name = types.StringValue(team.Name)
	plan.ManageState = types.BoolValue(team.ManageState)
	plan.ManageWorkspace = types.BoolValue(team.ManageWorkspace)
	plan.ManageModule = types.BoolValue(team.ManageModule)
	plan.ManageVcs = types.BoolValue(team.ManageVcs)
	plan.ManageProvider = types.BoolValue(team.ManageProvider)
	plan.ManageTemplate = types.BoolValue(team.ManageTemplate)
	plan.ManageJob = types.BoolValue(team.ManageJob)
	plan.ManageCollection = types.BoolValue(team.ManageCollection)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *TeamResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data TeamResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	reqOrg, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/organization/%s/team/%s", r.endpoint, data.OrganizationId.ValueString(), data.ID.ValueString()), nil)
	reqOrg.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	if err != nil {
		resp.Diagnostics.AddError("Error creating team resource request", fmt.Sprintf("Error creating team resource request: %s", err))
		return
	}

	_, err = r.client.Do(reqOrg)
	if err != nil {
		resp.Diagnostics.AddError("Error executing team resource request", fmt.Sprintf("Error executing team resource request: %s", err))
		return
	}
}

func (r *TeamResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
