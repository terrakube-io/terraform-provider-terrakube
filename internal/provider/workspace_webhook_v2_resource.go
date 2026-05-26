package provider

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"terraform-provider-terrakube/internal/client"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &WorkspaceWebhookV2Resource{}
var _ resource.ResourceWithImportState = &WorkspaceWebhookV2Resource{}

type WorkspaceWebhookV2Resource struct {
	client   *http.Client
	endpoint string
	token    string
}

type WorkspaceWebhookV2ResourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	WorkspaceId    types.String `tfsdk:"workspace_id"`
	RemoteHookId   types.String `tfsdk:"remote_hook_id"`
	MigratedV2     types.Bool   `tfsdk:"migrated_v2"`
}

func NewWorkspaceWebhookV2Resource() resource.Resource {
	return &WorkspaceWebhookV2Resource{}
}

func (r *WorkspaceWebhookV2Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_webhook_v2"
}

func (r *WorkspaceWebhookV2Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
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
			"remote_hook_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "The remote hook ID.",
			},
			"migrated_v2": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the webhook has been migrated to v2. Enables the webhook v2 processing path.",
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *WorkspaceWebhookV2Resource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *WorkspaceWebhookV2Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceWebhookV2ResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	webhookID := uuid.New().String()
	tflog.Debug(ctx, "Creating webhook request", map[string]any{
		"id": webhookID,
	})

	atomicOperation := map[string]interface{}{
		"atomic:operations": []map[string]interface{}{
			{
				"op":   "add",
				"href": fmt.Sprintf("/organization/%s/workspace/%s/webhook", plan.OrganizationId.ValueString(), plan.WorkspaceId.ValueString()),
				"data": map[string]interface{}{
					"type": "webhook",
					"id":   webhookID,
					"attributes": map[string]interface{}{
						"migratedV2": plan.MigratedV2.ValueBool(),
					},
				},
				"relationships": map[string]interface{}{
					"workspace": map[string]interface{}{
						"data": map[string]interface{}{
							"type": "workspace",
							"id":   plan.WorkspaceId.ValueString(),
						},
					},
				},
			},
		},
	}

	jsonData, err := json.Marshal(atomicOperation)
	if err != nil {
		tflog.Error(ctx, "Failed to marshal webhook payload", map[string]any{
			"error": err.Error(),
		})
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	tflog.Debug(ctx, "Marshaled webhook payload", map[string]any{
		"payload": string(jsonData),
	})

	request, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/operations", r.endpoint), strings.NewReader(string(jsonData)))
	if err != nil {
		tflog.Error(ctx, "Failed to create webhook request", map[string]any{
			"error": err.Error(),
		})
		resp.Diagnostics.AddError("Error creating workspace webhook resource request", fmt.Sprintf("Error creating workspace webhook resource request %s", err))
		return
	}

	request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", r.token))
	request.Header.Set("Content-Type", "application/vnd.api+json;ext=\"https://jsonapi.org/ext/atomic\"")
	request.Header.Set("Accept", "application/vnd.api+json;ext=\"https://jsonapi.org/ext/atomic\"")

	response, err := r.client.Do(request)
	if err != nil {
		tflog.Error(ctx, "Failed to execute webhook request", map[string]any{
			"error": err.Error(),
		})
		resp.Diagnostics.AddError("Error executing workspace webhook resource request", fmt.Sprintf("Error executing workspace webhook resource request, response status %s, response body: %s, error: %s", response.Status, response.Body, err))
		return
	}

	bodyResponse, err := io.ReadAll(response.Body)
	if err != nil {
		tflog.Error(ctx, "Failed to read webhook response", map[string]any{
			"error": err.Error(),
		})
		tflog.Error(ctx, fmt.Sprintf("Error reading workspace webhook resource, response status %s, response body: %s, error: %s", response.Status, response.Body, err))
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var errorResp ErrorResponse
		if err := json.Unmarshal(bodyResponse, &errorResp); err != nil {
			tflog.Error(ctx, "Failed to parse error response", map[string]any{
				"error": err.Error(),
				"body":  string(bodyResponse),
			})
			resp.Diagnostics.AddError(
				fmt.Sprintf("Failed to create/update webhook: %s", response.Status),
				string(bodyResponse),
			)
			return
		}

		// Decode HTML entities in the error message
		decodedDetail := html.UnescapeString(errorResp.Errors[0].Detail)

		tflog.Error(ctx, "API returned error status", map[string]any{
			"status_code": response.StatusCode,
			"body":        string(bodyResponse),
		})
		resp.Diagnostics.AddError(
			"Failed to create/update webhook",
			decodedDetail,
		)
		return
	}

	tflog.Debug(ctx, "Received webhook response", map[string]any{
		"status_code": response.StatusCode,
		"body":        string(bodyResponse),
	})

	var atomicResp AtomicOperationResponse
	if err := json.Unmarshal(bodyResponse, &atomicResp); err != nil {
		tflog.Error(ctx, "Failed to parse atomic operation response", map[string]any{
			"error": err.Error(),
			"body":  string(bodyResponse),
		})
		resp.Diagnostics.AddError(
			"Failed to parse webhook response",
			fmt.Sprintf("Error parsing webhook response: %s", err),
		)
		return
	}

	if len(atomicResp.AtomicResults) == 0 {
		resp.Diagnostics.AddError(
			"Invalid webhook response",
			"No results returned from atomic operation",
		)
		return
	}

	result := atomicResp.AtomicResults[0]
	plan.ID = types.StringValue(result.Data.ID)
	plan.RemoteHookId = types.StringValue(result.Data.ID) // Using ID as RemoteHookId since it's not in the response

	tflog.Debug(ctx, "Successfully created webhook", map[string]any{
		"id":             result.Data.ID,
		"remote_hook_id": result.Data.ID,
	})

	tflog.Info(ctx, "Workspace Webhook Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceWebhookV2Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceWebhookV2ResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	request, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/webhook/%s", r.endpoint, state.OrganizationId.ValueString(), state.WorkspaceId.ValueString(), state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace webhook resource request", fmt.Sprintf("Error creating workspace webhook resource request: %s", err))
		return
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")

	response, err := r.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace webhook resource request", fmt.Sprintf("Error executing workspace webhook resource request: %s", err))
		return
	}
	defer response.Body.Close()

	bodyResponse, err := io.ReadAll(response.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading workspace webhook resource response", fmt.Sprintf("Error reading workspace webhook resource response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	if response.StatusCode == http.StatusNotFound {
		resp.State.RemoveResource(ctx)
		return
	}

	if response.StatusCode != http.StatusOK {
		resp.Diagnostics.AddError("Error reading workspace webhook", fmt.Sprintf("Received non-200 status code: %d", response.StatusCode))
		return
	}

	var responseData struct {
		Data struct {
			Type       string `json:"type"`
			ID         string `json:"id"`
			Attributes struct {
				CreatedBy    string `json:"createdBy"`
				CreatedDate  string `json:"createdDate"`
				RemoteHookId string `json:"remoteHookId"`
				UpdatedBy    string `json:"updatedBy"`
				UpdatedDate  string `json:"updatedDate"`
				MigratedV2   bool   `json:"migratedV2"`
			} `json:"attributes"`
		} `json:"data"`
	}

	if err := json.Unmarshal(bodyResponse, &responseData); err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	state.ID = types.StringValue(responseData.Data.ID)
	state.RemoteHookId = types.StringValue(responseData.Data.Attributes.RemoteHookId)
	state.MigratedV2 = types.BoolValue(responseData.Data.Attributes.MigratedV2)

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Workspace Webhook Resource reading", map[string]any{"success": true})
}

func (r *WorkspaceWebhookV2Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan WorkspaceWebhookV2ResourceModel
	var state WorkspaceWebhookV2ResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	migratedV2 := plan.MigratedV2.ValueBool()
    bodyRequest := &client.WorkspaceWebhookV2Entity{
        ID:         state.ID.ValueString(),
        MigratedV2: &migratedV2,
    }

	jsonData, err := json.Marshal(bodyRequest)
	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	request, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/webhook/%s", r.endpoint, state.OrganizationId.ValueString(), state.WorkspaceId.ValueString(), state.ID.ValueString()), strings.NewReader(string(jsonData)))
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

	tflog.Info(ctx, "Body Response", map[string]any{"success": string(bodyResponse)})

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		tflog.Error(ctx, "API returned error status", map[string]any{
			"status_code": response.StatusCode,
			"body":        string(bodyResponse),
		})
		resp.Diagnostics.AddError(
			fmt.Sprintf("Failed to create/update webhook: %s", response.Status),
			string(bodyResponse),
		)
		return
	}

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

	var responseData struct {
		Data struct {
			Type       string `json:"type"`
			ID         string `json:"id"`
			Attributes struct {
				CreatedBy    string `json:"createdBy"`
				CreatedDate  string `json:"createdDate"`
				RemoteHookId string `json:"remoteHookId"`
				UpdatedBy    string `json:"updatedBy"`
				UpdatedDate  string `json:"updatedDate"`
				MigratedV2   bool   `json:"migratedV2"`
			} `json:"attributes"`
		} `json:"data"`
	}

	if err := json.Unmarshal(bodyResponse, &responseData); err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.RemoteHookId = types.StringValue(responseData.Data.Attributes.RemoteHookId)
	plan.MigratedV2 = types.BoolValue(responseData.Data.Attributes.MigratedV2)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceWebhookV2Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data WorkspaceWebhookV2ResourceModel

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

func (r *WorkspaceWebhookV2Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
