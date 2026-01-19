package provider

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"terraform-provider-terrakube/internal/client"
	"terraform-provider-terrakube/internal/helpers"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int32planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &TeamTokenResource{}
var _ resource.ResourceWithImportState = &TeamTokenResource{}

type TeamTokenResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type TeamTokenResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Group       types.String `tfsdk:"team_name"`
	Description types.String `tfsdk:"description"`
	Days        types.Int32  `tfsdk:"days"`
	Hours       types.Int32  `tfsdk:"hours"`
	Minutes     types.Int32  `tfsdk:"minutes"`
	Value       types.String `tfsdk:"value"`
}

func NewTeamTokenResource() resource.Resource {
	return &TeamTokenResource{}
}

func (r *TeamTokenResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_team_token"
}

func (r *TeamTokenResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a team token that shares the same privilege. Useful when managing devs access.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Team Token Id",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"team_name": schema.StringAttribute{
				Required:    true,
				Description: "The name of the team who owns the token.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				Required:    true,
				Description: "A description of this token.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"days": schema.Int32Attribute{
				Required:    true,
				Description: "The number of days this token is valid for.",
				PlanModifiers: []planmodifier.Int32{
					int32planmodifier.RequiresReplace(),
				},
			},
			"hours": schema.Int32Attribute{
				Required:    true,
				Description: "The number of hours this token is valid for.",
				PlanModifiers: []planmodifier.Int32{
					int32planmodifier.RequiresReplace(),
				},
			},
			"minutes": schema.Int32Attribute{
				Required:    true,
				Description: "The number of minutes this token is valid for.",
				PlanModifiers: []planmodifier.Int32{
					int32planmodifier.RequiresReplace(),
				},
			},
			"value": schema.StringAttribute{
				Computed:    true,
				Description: "The value of the token.",
				Sensitive:   true,
			},
		},
	}
}

func (r *TeamTokenResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Team Token Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Team Token resource finished successfully.", map[string]any{"success": true})
}

func (r *TeamTokenResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan TeamTokenResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.TeamTokenEntity{
		Description: plan.Description.ValueString(),
		Days:        plan.Days.ValueInt32(),
		Hours:       plan.Hours.ValueInt32(),
		Minutes:     plan.Minutes.ValueInt32(),
		Group:       plan.Group.ValueString(),
	}

	bodyJson, err := json.Marshal(bodyRequest)
	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal request ", fmt.Sprintf("Unable to marshal request, error: %s", err))
		return
	}

	teamTokenRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/access-token/v1/teams", r.endpoint), strings.NewReader(string(bodyJson)))
	teamTokenRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	teamTokenRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating team token resource request", fmt.Sprintf("Error creating team token resource request: %s", err))
		return
	}

	teamTokenResponse, err := r.client.Do(teamTokenRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing team token resource request", fmt.Sprintf("Error executing team token resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(teamTokenResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading team token resource response")
	}
	newTeamToken := &client.TeamTokenEntity{}

	err = json.Unmarshal(bodyResponse, newTeamToken)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response, error: %s, response status: %s", err, teamTokenResponse.Status))
		return
	}

	tflog.Info(ctx, "Body Response Status", map[string]any{"responseStatus": teamTokenResponse.Status})

	id, err := helpers.GetIDFromToken(newTeamToken.Value)
	if err != nil {
		resp.Diagnostics.AddError("Error getting claim from token", fmt.Sprintf("Error getting claim from token: %s", err))
	}
	plan.ID = types.StringValue(id)
	plan.Value = types.StringValue(newTeamToken.Value)

	tflog.Info(ctx, "Team Token Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *TeamTokenResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state TeamTokenResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	teamTokenRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/access-token/v1/teams", r.endpoint), nil)
	teamTokenRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	teamTokenRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating team token resource request", fmt.Sprintf("Error creating team token resource request: %s", err))
		return
	}

	teamTokenResponse, err := r.client.Do(teamTokenRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing team token resource request", fmt.Sprintf("Error executing team token resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(teamTokenResponse.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading team token resource response, error: %s, response status %s", err, teamTokenResponse.Status))
	}
	teamTokens := &[]client.TeamTokenEntity{}

	tflog.Debug(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	err = json.Unmarshal(bodyResponse, teamTokens)
	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response, error: %s, response status %s", err, teamTokenResponse.Status))
		return
	}

	tflog.Info(ctx, "Response status", map[string]any{"responseStatus": teamTokenResponse.Status})

	found := false
	for _, teamToken := range *teamTokens {
		if teamToken.ID != state.ID.ValueString() {
			continue
		}

		state.Description = types.StringValue(teamToken.Description)
		state.Days = types.Int32Value(teamToken.Days)
		state.Hours = types.Int32Value(teamToken.Hours)
		state.Minutes = types.Int32Value(teamToken.Minutes)
		state.Group = types.StringValue(teamToken.Group)
		found = true
		break
	}

	if !found {
		tflog.Warn(ctx, "Team token not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Team Token Resource reading finished", map[string]any{"success": true})
}

func (r *TeamTokenResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	tflog.Info(ctx, "Team token can't be updated but re-create.", map[string]any{"success": true})
}

func (r *TeamTokenResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data TeamTokenResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	reqToken, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/access-token/v1/teams/%s", r.endpoint, data.ID.ValueString()), nil)
	reqToken.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	if err != nil {
		resp.Diagnostics.AddError("Error deleting team token resource request", fmt.Sprintf("Error deleting team token resource request: %s", err))
		return
	}

	resToken, err := r.client.Do(reqToken)
	if err != nil || resToken.StatusCode != http.StatusAccepted {
		resp.Diagnostics.AddError("Error deleting team token", fmt.Sprintf("Error deleting team token, error: %s, response status: %s, response body: %s", err, resToken.Status, resToken.Body))
		return
	}
}

func (r *TeamTokenResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
