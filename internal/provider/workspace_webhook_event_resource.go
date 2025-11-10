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

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &WorkspaceWebhookEventResource{}
var _ resource.ResourceWithImportState = &WorkspaceWebhookEventResource{}

type WorkspaceWebhookEventResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type WorkspaceWebhookEventResourceModel struct {
	ID         types.String `tfsdk:"id"`
	WebhookId  types.String `tfsdk:"webhook_id"`
	Event      types.String `tfsdk:"event"`
	Branch     types.List   `tfsdk:"branch"`
	Path       types.List   `tfsdk:"path"`
	Priority   types.Int64  `tfsdk:"priority"`
	TemplateId types.String `tfsdk:"template_id"`
}

type webhookEventAPIResponse struct {
	Data []struct {
		Type       string `json:"type"`
		ID         string `json:"id"`
		Attributes struct {
			Branch      string `json:"branch"`
			Path        string `json:"path"`
			TemplateId  string `json:"templateId"`
			Event       string `json:"event"`
			Priority    int    `json:"priority"`
			CreatedBy   string `json:"createdBy"`
			CreatedDate string `json:"createdDate"`
			UpdatedBy   string `json:"updatedBy"`
			UpdatedDate string `json:"updatedDate"`
		} `json:"attributes"`
		Relationships struct {
			Webhook struct {
				Data struct {
					Type string `json:"type"`
					ID   string `json:"id"`
				} `json:"data"`
			} `json:"webhook"`
		} `json:"relationships"`
	} `json:"data"`
}

type webhookAPIResponse struct {
	Data struct {
		Type       string `json:"type"`
		ID         string `json:"id"`
		Attributes struct {
			Name string `json:"name"`
		} `json:"attributes"`
		Relationships struct {
			Organization struct {
				Data struct {
					Type string `json:"type"`
					ID   string `json:"id"`
				} `json:"data"`
			} `json:"organization"`
			Workspace struct {
				Data struct {
					Type string `json:"type"`
					ID   string `json:"id"`
				} `json:"data"`
			} `json:"workspace"`
			Events struct {
				Data []struct {
					Type string `json:"type"`
					ID   string `json:"id"`
				} `json:"data"`
			} `json:"events"`
		} `json:"relationships"`
	} `json:"data"`
}

func NewWorkspaceWebhookEventResource() resource.Resource {
	return &WorkspaceWebhookEventResource{}
}

func (r *WorkspaceWebhookEventResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_webhook_event"
}

func (r *WorkspaceWebhookEventResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a webhook event attached to a webhook. Defines when and how the webhook should trigger.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Webhook Event ID",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"webhook_id": schema.StringAttribute{
				Required:    true,
				Description: "The ID of the webhook this event belongs to",
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
			"priority": schema.Int64Attribute{
				Optional:    true,
				Description: "The priority of this webhook event",
				Computed:    true,
			},
			"template_id": schema.StringAttribute{
				Optional:    true,
				Description: "The template id to use for the run.",
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

func (r *WorkspaceWebhookEventResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Workspace Webhook Event Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Webhook Event resource", map[string]any{"success": true})
}

func (r *WorkspaceWebhookEventResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceWebhookEventResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// First, get the webhook details to get organization and workspace IDs
	webhookRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/webhook/%s", r.endpoint, plan.WebhookId.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating webhook read request", fmt.Sprintf("Error creating webhook read request: %s", err))
		return
	}

	webhookRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	webhookRequest.Header.Add("Content-Type", "application/vnd.api+json")

	webhookResponse, err := r.client.Do(webhookRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing webhook read request", fmt.Sprintf("Error executing webhook read request: %s", err))
		return
	}

	webhookBody, err := io.ReadAll(webhookResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading webhook response", fmt.Sprintf("Error reading webhook response: %s", err))
		return
	}

	tflog.Debug(ctx, "Read webhook response", map[string]any{
		"status_code": webhookResponse.StatusCode,
		"body":        string(webhookBody),
	})

	if webhookResponse.StatusCode == http.StatusNotFound {
		resp.Diagnostics.AddError("Webhook not found", fmt.Sprintf("Webhook with ID %s not found", plan.WebhookId.ValueString()))
		return
	}

	var webhookResp webhookAPIResponse
	if err := json.Unmarshal(webhookBody, &webhookResp); err != nil {
		resp.Diagnostics.AddError("Error parsing webhook response", fmt.Sprintf("Error parsing webhook response: %s", err))
		return
	}

	eventID := uuid.New().String()
	tflog.Debug(ctx, "Creating webhook event request", map[string]any{
		"id": eventID,
	})

	var branchList, pathList []string
	if !plan.Branch.IsNull() && !plan.Branch.IsUnknown() {
		resp.Diagnostics.Append(plan.Branch.ElementsAs(ctx, &branchList, false)...)
	}
	if !plan.Path.IsNull() && !plan.Path.IsUnknown() {
		resp.Diagnostics.Append(plan.Path.ElementsAs(ctx, &pathList, false)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	atomicOperation := map[string]interface{}{
		"atomic:operations": []map[string]interface{}{
			{
				"op":   "add",
				"href": fmt.Sprintf("/webhook/%s/events", plan.WebhookId.ValueString()),
				"data": map[string]interface{}{
					"type": "webhook_event",
					"id":   eventID,
					"attributes": map[string]interface{}{
						"priority":   plan.Priority.ValueInt64(),
						"event":      plan.Event.ValueString(),
						"branch":     strings.Join(branchList, ","),
						"path":       strings.Join(pathList, ","),
						"templateId": plan.TemplateId.ValueString(),
					},
				},
			},
		},
	}

	jsonData, err := json.Marshal(atomicOperation)
	if err != nil {
		tflog.Error(ctx, "Failed to marshal webhook event payload", map[string]any{
			"error": err.Error(),
		})
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	tflog.Debug(ctx, "Marshaled webhook event payload", map[string]any{
		"payload": string(jsonData),
	})

	request, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/operations", r.endpoint), strings.NewReader(string(jsonData)))
	if err != nil {
		tflog.Error(ctx, "Failed to create webhook event request", map[string]any{
			"error": err.Error(),
		})
		resp.Diagnostics.AddError("Error creating workspace webhook event resource request", fmt.Sprintf("Error creating workspace webhook event resource request %s", err))
		return
	}

	request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", r.token))
	request.Header.Set("Content-Type", "application/vnd.api+json;ext=\"https://jsonapi.org/ext/atomic\"")
	request.Header.Set("Accept", "application/vnd.api+json;ext=\"https://jsonapi.org/ext/atomic\"")

	response, err := r.client.Do(request)
	if err != nil {
		tflog.Error(ctx, "Failed to execute webhook event request", map[string]any{
			"error": err.Error(),
		})
		resp.Diagnostics.AddError("Error executing workspace webhook event resource request", fmt.Sprintf("Error executing workspace webhook event resource request, response status %s, response body: %s, error: %s", response.Status, response.Body, err))
		return
	}

	bodyResponse, err := io.ReadAll(response.Body)
	if err != nil {
		tflog.Error(ctx, "Failed to read webhook event response", map[string]any{
			"error": err.Error(),
		})
		tflog.Error(ctx, fmt.Sprintf("Error reading workspace webhook event resource, response status %s, response body: %s, error: %s", response.Status, response.Body, err))
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var errorResp ErrorResponse
		if err := json.Unmarshal(bodyResponse, &errorResp); err != nil {
			tflog.Error(ctx, "Failed to parse error response", map[string]any{
				"error": err.Error(),
				"body":  string(bodyResponse),
			})
			resp.Diagnostics.AddError(
				fmt.Sprintf("Failed to create/update webhook event: %s", response.Status),
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
			"Failed to create/update webhook event",
			decodedDetail,
		)
		return
	}

	tflog.Debug(ctx, "Received webhook event response", map[string]any{
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
			"Failed to parse webhook event response",
			fmt.Sprintf("Error parsing webhook event response: %s", err),
		)
		return
	}

	if len(atomicResp.AtomicResults) == 0 {
		resp.Diagnostics.AddError(
			"Invalid webhook event response",
			"No results returned from atomic operation",
		)
		return
	}

	result := atomicResp.AtomicResults[0]
	plan.ID = types.StringValue(result.Data.ID)

	tflog.Info(ctx, "Workspace Webhook Event Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceWebhookEventResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data WorkspaceWebhookEventResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// First, get the webhook details to get organization and workspace IDs
	webhookRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/webhook/%s", r.endpoint, data.WebhookId.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating webhook read request", fmt.Sprintf("Error creating webhook read request: %s", err))
		return
	}

	webhookRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	webhookRequest.Header.Add("Content-Type", "application/vnd.api+json")

	webhookResponse, err := r.client.Do(webhookRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing webhook read request", fmt.Sprintf("Error executing webhook read request: %s", err))
		return
	}

	webhookBody, err := io.ReadAll(webhookResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading webhook response", fmt.Sprintf("Error reading webhook response: %s", err))
		return
	}

	tflog.Debug(ctx, "Read webhook response", map[string]any{
		"status_code": webhookResponse.StatusCode,
		"body":        string(webhookBody),
	})

	if webhookResponse.StatusCode == http.StatusNotFound {
		// If the webhook is not found, we can consider the event deleted
		tflog.Info(ctx, "Webhook not found, considering event deleted", map[string]any{"success": true})
		return
	}

	var webhookDetails webhookAPIResponse
	if err := json.Unmarshal(webhookBody, &webhookDetails); err != nil {
		resp.Diagnostics.AddError("Error parsing webhook response", fmt.Sprintf("Error parsing webhook response: %s", err))
		return
	}

	// Check if the event still exists in the webhook's events relationship
	eventFound := false
	for _, event := range webhookDetails.Data.Relationships.Events.Data {
		if event.ID == data.ID.ValueString() {
			eventFound = true
			break
		}
	}

	if !eventFound {
		// If the event is not found in the webhook's events, we can consider it deleted
		tflog.Info(ctx, "Event not found in webhook's events, considering it deleted", map[string]any{"success": true})
		return
	}

	// Use atomic operation to delete the event
	atomicOperation := map[string]interface{}{
		"atomic:operations": []map[string]interface{}{
			{
				"op": "remove",
				"href": fmt.Sprintf("/webhook/%s/events/%s",
					data.WebhookId.ValueString(),
					data.ID.ValueString()),
			},
		},
	}

	jsonData, err := json.Marshal(atomicOperation)
	if err != nil {
		tflog.Error(ctx, "Failed to marshal webhook event delete payload", map[string]any{
			"error": err.Error(),
		})
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	tflog.Debug(ctx, "Marshaled webhook event delete payload", map[string]any{
		"payload": string(jsonData),
	})

	request, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/operations", r.endpoint), strings.NewReader(string(jsonData)))
	if err != nil {
		tflog.Error(ctx, "Failed to create webhook event delete request", map[string]any{
			"error": err.Error(),
		})
		resp.Diagnostics.AddError("Error creating workspace webhook event resource request", fmt.Sprintf("Error creating workspace webhook event resource request %s", err))
		return
	}

	request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", r.token))
	request.Header.Set("Content-Type", "application/vnd.api+json;ext=\"https://jsonapi.org/ext/atomic\"")
	request.Header.Set("Accept", "application/vnd.api+json;ext=\"https://jsonapi.org/ext/atomic\"")

	response, err := r.client.Do(request)
	if err != nil {
		tflog.Error(ctx, "Failed to execute webhook event delete request", map[string]any{
			"error": err.Error(),
		})
		resp.Diagnostics.AddError("Error executing workspace webhook event resource request", fmt.Sprintf("Error executing workspace webhook event resource request, response status %s, response body: %s, error: %s", response.Status, response.Body, err))
		return
	}

	bodyResponse, err := io.ReadAll(response.Body)
	if err != nil {
		tflog.Error(ctx, "Failed to read webhook event delete response", map[string]any{
			"error": err.Error(),
		})
		tflog.Error(ctx, fmt.Sprintf("Error reading workspace webhook event resource, response status %s, response body: %s, error: %s", response.Status, response.Body, err))
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var errorResp ErrorResponse
		if err := json.Unmarshal(bodyResponse, &errorResp); err != nil {
			tflog.Error(ctx, "Failed to parse error response", map[string]any{
				"error": err.Error(),
				"body":  string(bodyResponse),
			})
			resp.Diagnostics.AddError(
				fmt.Sprintf("Failed to delete webhook event: %s", response.Status),
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
			"Failed to delete webhook event",
			decodedDetail,
		)
		return
	}

	tflog.Info(ctx, "Workspace Webhook Event Resource Deleted", map[string]any{"success": true})
}

func (r *WorkspaceWebhookEventResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceWebhookEventResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// First, get the webhook details to get organization and workspace IDs
	webhookRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/webhook/%s", r.endpoint, state.WebhookId.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating webhook read request", fmt.Sprintf("Error creating webhook read request: %s", err))
		return
	}

	webhookRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	webhookRequest.Header.Add("Content-Type", "application/vnd.api+json")

	webhookResponse, err := r.client.Do(webhookRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing webhook read request", fmt.Sprintf("Error executing webhook read request: %s", err))
		return
	}

	webhookBody, err := io.ReadAll(webhookResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading webhook response", fmt.Sprintf("Error reading webhook response: %s", err))
		return
	}

	tflog.Debug(ctx, "Read webhook response", map[string]any{
		"status_code": webhookResponse.StatusCode,
		"body":        string(webhookBody),
	})

	if webhookResponse.StatusCode == http.StatusNotFound {
		resp.State.RemoveResource(ctx)
		return
	}

	var webhookDetails webhookAPIResponse
	if err := json.Unmarshal(webhookBody, &webhookDetails); err != nil {
		resp.Diagnostics.AddError("Error parsing webhook response", fmt.Sprintf("Error parsing webhook response: %s", err))
		return
	}

	tflog.Debug(ctx, "Parsed webhook details", map[string]any{
		"organization_id": webhookDetails.Data.Relationships.Organization.Data.ID,
		"workspace_id":    webhookDetails.Data.Relationships.Workspace.Data.ID,
		"webhook_id":      webhookDetails.Data.ID,
	})

	workspaceId := webhookDetails.Data.Relationships.Workspace.Data.ID
	if workspaceId == "" {
		resp.Diagnostics.AddError("Invalid webhook response", "Could not determine Workspace ID from webhook response")
		return
	}

	// Get organization ID from workspace
	workspaceRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/workspace/%s", r.endpoint, workspaceId), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace request", fmt.Sprintf("Error creating workspace request: %s", err))
		return
	}

	workspaceRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceRequest.Header.Add("Content-Type", "application/vnd.api+json")

	workspaceResponse, err := r.client.Do(workspaceRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace request", fmt.Sprintf("Error executing workspace request: %s", err))
		return
	}

	workspaceBody, err := io.ReadAll(workspaceResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading workspace response", fmt.Sprintf("Error reading workspace response: %s", err))
		return
	}

	tflog.Debug(ctx, "Read workspace response", map[string]any{
		"status_code": workspaceResponse.StatusCode,
		"body":        string(workspaceBody),
	})

	if workspaceResponse.StatusCode == http.StatusNotFound {
		resp.Diagnostics.AddError("Workspace not found", fmt.Sprintf("Workspace with ID %s not found", workspaceId))
		return
	}

	var workspaceResp struct {
		Data struct {
			Type       string `json:"type"`
			ID         string `json:"id"`
			Attributes struct {
				Name string `json:"name"`
			} `json:"attributes"`
			Relationships struct {
				Organization struct {
					Data struct {
						Type string `json:"type"`
						ID   string `json:"id"`
					} `json:"data"`
				} `json:"organization"`
			} `json:"relationships"`
		} `json:"data"`
	}

	if err := json.Unmarshal(workspaceBody, &workspaceResp); err != nil {
		resp.Diagnostics.AddError("Error parsing workspace response", fmt.Sprintf("Error parsing workspace response: %s", err))
		return
	}

	organizationId := workspaceResp.Data.Relationships.Organization.Data.ID
	if organizationId == "" {
		resp.Diagnostics.AddError("Invalid workspace response", "Could not determine Organization ID from workspace response")
		return
	}

	tflog.Debug(ctx, "Parsed workspace details", map[string]any{
		"organization_id": organizationId,
		"workspace_id":    workspaceId,
	})

	var branchList, pathList []string
	if !state.Branch.IsNull() && !state.Branch.IsUnknown() {
		resp.Diagnostics.Append(state.Branch.ElementsAs(ctx, &branchList, false)...)
	}
	if !state.Path.IsNull() && !state.Path.IsUnknown() {
		resp.Diagnostics.Append(state.Path.ElementsAs(ctx, &pathList, false)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	// Check if the event ID exists in the webhook's events relationship
	eventFound := false
	for _, event := range webhookDetails.Data.Relationships.Events.Data {
		if event.ID == state.ID.ValueString() {
			eventFound = true
			break
		}
	}

	if !eventFound {
		resp.State.RemoveResource(ctx)
		return
	}

	// Since we found the event in the webhook's relationships, we can keep the state as is
	// The event details are not critical for the resource to function
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Workspace Webhook Event Resource reading", map[string]any{"success": true})
}

func (r *WorkspaceWebhookEventResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan WorkspaceWebhookEventResourceModel
	var state WorkspaceWebhookEventResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// First, get the webhook details to get organization and workspace IDs
	webhookRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/webhook/%s", r.endpoint, state.WebhookId.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating webhook read request", fmt.Sprintf("Error creating webhook read request: %s", err))
		return
	}

	webhookRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	webhookRequest.Header.Add("Content-Type", "application/vnd.api+json")

	webhookResponse, err := r.client.Do(webhookRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing webhook read request", fmt.Sprintf("Error executing webhook read request: %s", err))
		return
	}

	webhookBody, err := io.ReadAll(webhookResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading webhook response", fmt.Sprintf("Error reading webhook response: %s", err))
		return
	}

	if webhookResponse.StatusCode == http.StatusNotFound {
		resp.Diagnostics.AddError("Webhook not found", fmt.Sprintf("Webhook with ID %s not found", state.WebhookId.ValueString()))
		return
	}

	var webhookDetails webhookAPIResponse
	if err := json.Unmarshal(webhookBody, &webhookDetails); err != nil {
		resp.Diagnostics.AddError("Error parsing webhook response", fmt.Sprintf("Error parsing webhook response: %s", err))
		return
	}

	tflog.Debug(ctx, "Parsed webhook details", map[string]any{
		"organization_id": webhookDetails.Data.Relationships.Organization.Data.ID,
		"workspace_id":    webhookDetails.Data.Relationships.Workspace.Data.ID,
		"webhook_id":      webhookDetails.Data.ID,
	})

	workspaceId := webhookDetails.Data.Relationships.Workspace.Data.ID
	if workspaceId == "" {
		resp.Diagnostics.AddError("Invalid webhook response", "Could not determine Workspace ID from webhook response")
		return
	}

	// Get organization ID from workspace
	workspaceRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/workspace/%s", r.endpoint, workspaceId), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace request", fmt.Sprintf("Error creating workspace request: %s", err))
		return
	}

	workspaceRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceRequest.Header.Add("Content-Type", "application/vnd.api+json")

	workspaceResponse, err := r.client.Do(workspaceRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace request", fmt.Sprintf("Error executing workspace request: %s", err))
		return
	}

	workspaceBody, err := io.ReadAll(workspaceResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading workspace response", fmt.Sprintf("Error reading workspace response: %s", err))
		return
	}

	tflog.Debug(ctx, "Read workspace response", map[string]any{
		"status_code": workspaceResponse.StatusCode,
		"body":        string(workspaceBody),
	})

	if workspaceResponse.StatusCode == http.StatusNotFound {
		resp.Diagnostics.AddError("Workspace not found", fmt.Sprintf("Workspace with ID %s not found", workspaceId))
		return
	}

	var workspaceResp struct {
		Data struct {
			Type       string `json:"type"`
			ID         string `json:"id"`
			Attributes struct {
				Name string `json:"name"`
			} `json:"attributes"`
			Relationships struct {
				Organization struct {
					Data struct {
						Type string `json:"type"`
						ID   string `json:"id"`
					} `json:"data"`
				} `json:"organization"`
			} `json:"relationships"`
		} `json:"data"`
	}

	if err := json.Unmarshal(workspaceBody, &workspaceResp); err != nil {
		resp.Diagnostics.AddError("Error parsing workspace response", fmt.Sprintf("Error parsing workspace response: %s", err))
		return
	}

	organizationId := workspaceResp.Data.Relationships.Organization.Data.ID
	if organizationId == "" {
		resp.Diagnostics.AddError("Invalid workspace response", "Could not determine Organization ID from workspace response")
		return
	}

	tflog.Debug(ctx, "Parsed workspace details", map[string]any{
		"organization_id": organizationId,
		"workspace_id":    workspaceId,
	})

	var branchList, pathList []string
	if !plan.Branch.IsNull() && !plan.Branch.IsUnknown() {
		resp.Diagnostics.Append(plan.Branch.ElementsAs(ctx, &branchList, false)...)
	}
	if !plan.Path.IsNull() && !plan.Path.IsUnknown() {
		resp.Diagnostics.Append(plan.Path.ElementsAs(ctx, &pathList, false)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	atomicOperation := map[string]interface{}{
		"atomic:operations": []map[string]interface{}{
			{
				"op": "update",
				"href": fmt.Sprintf("/organization/%s/workspace/%s/webhook/%s/events/%s",
					organizationId,
					workspaceId,
					state.WebhookId.ValueString(),
					state.ID.ValueString()),
				"data": map[string]interface{}{
					"type": "webhook_event",
					"id":   state.ID.ValueString(),
					"attributes": map[string]interface{}{
						"priority":   plan.Priority.ValueInt64(),
						"event":      plan.Event.ValueString(),
						"branch":     strings.Join(branchList, ","),
						"path":       strings.Join(pathList, ","),
						"templateId": plan.TemplateId.ValueString(),
					},
				},
			},
		},
	}

	jsonData, err := json.Marshal(atomicOperation)
	if err != nil {
		tflog.Error(ctx, "Failed to marshal webhook event payload", map[string]any{
			"error": err.Error(),
		})
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	tflog.Debug(ctx, "Marshaled webhook event payload", map[string]any{
		"payload": string(jsonData),
	})

	request, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/operations", r.endpoint), strings.NewReader(string(jsonData)))
	if err != nil {
		tflog.Error(ctx, "Failed to create webhook event request", map[string]any{
			"error": err.Error(),
		})
		resp.Diagnostics.AddError("Error creating workspace webhook event resource request", fmt.Sprintf("Error creating workspace webhook event resource request %s", err))
		return
	}

	request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", r.token))
	request.Header.Set("Content-Type", "application/vnd.api+json;ext=\"https://jsonapi.org/ext/atomic\"")
	request.Header.Set("Accept", "application/vnd.api+json;ext=\"https://jsonapi.org/ext/atomic\"")

	response, err := r.client.Do(request)
	if err != nil {
		tflog.Error(ctx, "Failed to execute webhook event request", map[string]any{
			"error": err.Error(),
		})
		resp.Diagnostics.AddError("Error executing workspace webhook event resource request", fmt.Sprintf("Error executing workspace webhook event resource request, response status %s, response body: %s, error: %s", response.Status, response.Body, err))
		return
	}

	bodyResponse, err := io.ReadAll(response.Body)
	if err != nil {
		tflog.Error(ctx, "Failed to read webhook event response", map[string]any{
			"error": err.Error(),
		})
		tflog.Error(ctx, fmt.Sprintf("Error reading workspace webhook event resource, response status %s, response body: %s, error: %s", response.Status, response.Body, err))
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var errorResp ErrorResponse
		if err := json.Unmarshal(bodyResponse, &errorResp); err != nil {
			tflog.Error(ctx, "Failed to parse error response", map[string]any{
				"error": err.Error(),
				"body":  string(bodyResponse),
			})
			resp.Diagnostics.AddError(
				fmt.Sprintf("Failed to create/update webhook event: %s", response.Status),
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
			"Failed to create/update webhook event",
			decodedDetail,
		)
		return
	}

	tflog.Debug(ctx, "Received webhook event response", map[string]any{
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
			"Failed to parse webhook event response",
			fmt.Sprintf("Error parsing webhook event response: %s", err),
		)
		return
	}

	if len(atomicResp.AtomicResults) == 0 {
		resp.Diagnostics.AddError(
			"Invalid webhook event response",
			"No results returned from atomic operation",
		)
		return
	}

	// Read the updated webhook event to get all attributes
	request, err = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/webhook/%s/events",
		r.endpoint,
		organizationId,
		workspaceId,
		state.WebhookId.ValueString()), nil)
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating webhook event read request", fmt.Sprintf("Error creating webhook event read request: %s", err))
		return
	}

	response, err = r.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Error executing webhook event read request", fmt.Sprintf("Error executing webhook event read request: %s", err))
		return
	}

	bodyResponse, err = io.ReadAll(response.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading webhook event response", fmt.Sprintf("Error reading webhook event response: %s", err))
		return
	}

	var eventsResp webhookEventAPIResponse
	err = json.Unmarshal(bodyResponse, &eventsResp)
	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	// Find the event with matching ID
	var foundEvent *struct {
		Type       string `json:"type"`
		ID         string `json:"id"`
		Attributes struct {
			Branch      string `json:"branch"`
			Path        string `json:"path"`
			TemplateId  string `json:"templateId"`
			Event       string `json:"event"`
			Priority    int    `json:"priority"`
			CreatedBy   string `json:"createdBy"`
			CreatedDate string `json:"createdDate"`
			UpdatedBy   string `json:"updatedBy"`
			UpdatedDate string `json:"updatedDate"`
		} `json:"attributes"`
		Relationships struct {
			Webhook struct {
				Data struct {
					Type string `json:"type"`
					ID   string `json:"id"`
				} `json:"data"`
			} `json:"webhook"`
		} `json:"relationships"`
	}

	for _, event := range eventsResp.Data {
		if event.ID == state.ID.ValueString() {
			foundEvent = &event
			break
		}
	}

	if foundEvent == nil {
		resp.Diagnostics.AddError("Error updating webhook event", "Could not find updated event in response")
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.Path, _ = types.ListValueFrom(ctx, types.StringType, strings.Split(foundEvent.Attributes.Path, ","))
	plan.Branch, _ = types.ListValueFrom(ctx, types.StringType, strings.Split(foundEvent.Attributes.Branch, ","))
	plan.TemplateId = types.StringValue(foundEvent.Attributes.TemplateId)
	plan.Event = types.StringValue(foundEvent.Attributes.Event)
	plan.Priority = types.Int64Value(int64(foundEvent.Attributes.Priority))

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceWebhookEventResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) != 2 || idParts[0] == "" || idParts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: 'webhook_ID,event_ID', Got: %q", req.ID),
		)
		return
	}

	// Use path.Root directly to make the import usage more explicit
	webhookIdPath := path.Root("webhook_id")
	idPath := path.Root("id")

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, webhookIdPath, idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, idPath, idParts[1])...)
}
