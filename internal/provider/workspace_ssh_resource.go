package provider

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"terraform-provider-terrakube/internal/client"
	"time"

	"github.com/google/jsonapi"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ resource.Resource = &WorkspaceSshResource{}
var _ resource.ResourceWithImportState = &WorkspaceSshResource{}

type WorkspaceSshResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type WorkspaceSshResourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	WorkspaceId    types.String `tfsdk:"workspace_id"`
	SshId          types.String `tfsdk:"ssh_id"`
}

func NewWorkspaceSshResource() resource.Resource {
	return &WorkspaceSshResource{}
}

func (r *WorkspaceSshResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_ssh"
}

func (r *WorkspaceSshResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Link an organization SSH key to a workspace.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Workspace SSH link id",
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
			"ssh_id": schema.StringAttribute{
				Required:    true,
				Description: "Organization SSH key id to attach to workspace",
			},
		},
	}
}

func (r *WorkspaceSshResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Workspace SSH Resource Configure Type",
			fmt.Sprintf("Expected *TerrakubeConnectionData, got: %T.", req.ProviderData),
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
	// Log provider executable path and timestamp to help ensure Terraform is loading the expected binary
	exePath, _ := os.Executable()
	var exeMod time.Time
	if info, err := os.Stat(exePath); err == nil {
		exeMod = info.ModTime()
	}
	tflog.Info(ctx, "Configuring Workspace SSH resource", map[string]any{"success": true, "exe": exePath, "exe_mod": exeMod.String()})
}

func (r *WorkspaceSshResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	tflog.Info(ctx, "Workspace SSH Create called", map[string]any{"time": time.Now().UTC().String()})
	var plan WorkspaceSshResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Verify SSH key exists in organization
	sshCheckReq, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/ssh/%s", r.endpoint, plan.OrganizationId.ValueString(), plan.SshId.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating SSH check request", fmt.Sprintf("Error creating request: %s", err))
		return
	}
	sshCheckReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	sshCheckResp, err := r.client.Do(sshCheckReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing SSH check request", fmt.Sprintf("Error executing request: %s", err))
		return
	}
	if sshCheckResp.StatusCode == http.StatusNotFound {
		resp.Diagnostics.AddError("SSH key not found", fmt.Sprintf("SSH key %s not found in organization %s", plan.SshId.ValueString(), plan.OrganizationId.ValueString()))
		return
	}
	// Compose JSON: set workspace attribute `moduleSshKey` to ssh id by PATCHing the workspace
	payload := fmt.Sprintf(`{"data":{"type":"workspace","id":"%s","attributes":{"moduleSshKey":"%s"}}}`, plan.WorkspaceId.ValueString(), plan.SshId.ValueString())

	attachRequest, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s", r.endpoint, plan.OrganizationId.ValueString(), plan.WorkspaceId.ValueString()), strings.NewReader(payload))
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace ssh attach request", fmt.Sprintf("Error creating request: %s", err))
		return
	}
	attachRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	attachRequest.Header.Add("Content-Type", "application/vnd.api+json")

	attachResponse, err := r.client.Do(attachRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace ssh attach request", fmt.Sprintf("Error executing request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(attachResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading workspace ssh attach response")
	}

	// Expect 200/201/204 for successful attach
	if attachResponse.StatusCode != http.StatusOK && attachResponse.StatusCode != http.StatusCreated && attachResponse.StatusCode != http.StatusNoContent {
		resp.Diagnostics.AddError("Error attaching SSH to workspace", fmt.Sprintf("Unexpected response status: %d, body: %s", attachResponse.StatusCode, string(bodyResponse)))
		return
	}

	tflog.Info(ctx, "Workspace SSH attach response", map[string]any{"status": attachResponse.StatusCode, "body": string(bodyResponse)})

	// Verify the workspace now references the moduleSshKey
	verifyReq, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s", r.endpoint, plan.OrganizationId.ValueString(), plan.WorkspaceId.ValueString()), nil)
	if err == nil {
		verifyReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
		verifyReq.Header.Add("Content-Type", "application/vnd.api+json")
		verifyResp, err := r.client.Do(verifyReq)
		if err == nil {
			body, _ := io.ReadAll(verifyResp.Body)
			if !strings.Contains(string(body), plan.SshId.ValueString()) {
				resp.Diagnostics.AddError("moduleSshKey not set", fmt.Sprintf("Workspace does not reference moduleSshKey %s after attach. API response body: %s", plan.SshId.ValueString(), string(body)))
				return
			}
		}
	}

	plan.ID = types.StringValue(fmt.Sprintf("%s,%s", plan.WorkspaceId.ValueString(), plan.SshId.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceSshResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	tflog.Info(ctx, "Workspace SSH Read called", map[string]any{"time": time.Now().UTC().String()})
	var state WorkspaceSshResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s", r.endpoint, state.OrganizationId.ValueString(), state.WorkspaceId.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace read request", fmt.Sprintf("Error creating request: %s", err))
		return
	}
	workspaceRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	workspaceRequest.Header.Add("Content-Type", "application/vnd.api+json")

	workspaceResponse, err := r.client.Do(workspaceRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace read request", fmt.Sprintf("Error executing request: %s", err))
		return
	}
	defer workspaceResponse.Body.Close()

	if workspaceResponse.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Workspace not found, removing from state", map[string]any{"id": state.WorkspaceId.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	if workspaceResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(workspaceResponse.Body)
		resp.Diagnostics.AddError("Error reading workspace state", fmt.Sprintf("Unexpected response status: %d, body: %s", workspaceResponse.StatusCode, string(body)))
		return
	}

	bodyResponse, err := io.ReadAll(workspaceResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading workspace response")
		resp.Diagnostics.AddError("Error reading workspace response", fmt.Sprintf("Error reading response body: %s", err))
		return
	}

	workspace := &client.WorkspaceEntity{}
	_ = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), workspace)

	// Try to read moduleSshKey from the live workspace attributes so Terraform plan
	// sees drift between state and live resource. API returns JSON: data.attributes.moduleSshKey
	var parsed map[string]interface{}
	sshFound := false
	if err := json.Unmarshal(bodyResponse, &parsed); err == nil {
		if data, ok := parsed["data"].(map[string]interface{}); ok {
			if attrs, ok := data["attributes"].(map[string]interface{}); ok {
				if v, ok := attrs["moduleSshKey"]; ok {
					switch val := v.(type) {
					case string:
						if val != "" {
							sshFound = true
							state.SshId = types.StringValue(val)
						} else {
							sshFound = false
							state.SshId = types.StringNull()
						}
					case nil:
						sshFound = false
						state.SshId = types.StringNull()
					default:
						sshFound = false
						state.SshId = types.StringNull()
					}
				}
			}
		}
	}

	if !sshFound {
		tflog.Debug(ctx, "Workspace SSH link not present in live state, removing resource state", map[string]any{"workspace_id": state.WorkspaceId.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	// Set refreshed state
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *WorkspaceSshResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	tflog.Info(ctx, "Workspace SSH Update called", map[string]any{"time": time.Now().UTC().String()})
	var plan WorkspaceSshResourceModel
	var state WorkspaceSshResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// If ssh id didn't change, nothing to do
	if !state.SshId.IsNull() && state.SshId.ValueString() == plan.SshId.ValueString() {
		// still update composed id if workspace or ssh changed
		plan.ID = types.StringValue(fmt.Sprintf("%s,%s", plan.WorkspaceId.ValueString(), plan.SshId.ValueString()))
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	// If there was no previous ssh linked, PATCH workspace to set moduleSshKey (same as Create)
	if state.SshId.IsNull() || state.SshId.ValueString() == "" {
		// Verify SSH exists before attaching
		sshCheckReq, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/ssh/%s", r.endpoint, plan.OrganizationId.ValueString(), plan.SshId.ValueString()), nil)
		if err != nil {
			resp.Diagnostics.AddError("Error creating SSH check request", fmt.Sprintf("Error creating request: %s", err))
			return
		}
		sshCheckReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
		sshCheckResp, err := r.client.Do(sshCheckReq)
		if err != nil {
			resp.Diagnostics.AddError("Error executing SSH check request", fmt.Sprintf("Error executing request: %s", err))
			return
		}
		if sshCheckResp.StatusCode == http.StatusNotFound {
			resp.Diagnostics.AddError("SSH key not found", fmt.Sprintf("SSH key %s not found in organization %s", plan.SshId.ValueString(), plan.OrganizationId.ValueString()))
			return
		}

		payload := fmt.Sprintf(`{"data":{"type":"workspace","id":"%s","attributes":{"moduleSshKey":"%s"}}}`, plan.WorkspaceId.ValueString(), plan.SshId.ValueString())

		attachRequest, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s", r.endpoint, plan.OrganizationId.ValueString(), plan.WorkspaceId.ValueString()), strings.NewReader(payload))
		if err != nil {
			resp.Diagnostics.AddError("Error creating workspace ssh attach request", fmt.Sprintf("Error creating request: %s", err))
			return
		}
		attachRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
		attachRequest.Header.Add("Content-Type", "application/vnd.api+json")

		attachResponse, err := r.client.Do(attachRequest)
		if err != nil {
			resp.Diagnostics.AddError("Error executing workspace ssh attach request", fmt.Sprintf("Error executing request: %s", err))
			return
		}

		bodyResponse, err := io.ReadAll(attachResponse.Body)
		if err != nil {
			tflog.Error(ctx, "Error reading workspace ssh attach response")
		}

		if attachResponse.StatusCode != http.StatusOK && attachResponse.StatusCode != http.StatusCreated && attachResponse.StatusCode != http.StatusNoContent {
			resp.Diagnostics.AddError("Error attaching SSH to workspace", fmt.Sprintf("Unexpected response status: %d, body: %s", attachResponse.StatusCode, string(bodyResponse)))
			return
		}

		plan.ID = types.StringValue(fmt.Sprintf("%s,%s", plan.WorkspaceId.ValueString(), plan.SshId.ValueString()))
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	// Otherwise PATCH the workspace to update moduleSshKey attribute to the new ssh id
	payload := fmt.Sprintf(`{"data":{"type":"workspace","id":"%s","attributes":{"moduleSshKey":"%s"}}}`, plan.WorkspaceId.ValueString(), plan.SshId.ValueString())

	patchReq, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s", r.endpoint, state.OrganizationId.ValueString(), state.WorkspaceId.ValueString()), strings.NewReader(payload))
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace ssh patch request", fmt.Sprintf("Error creating request: %s", err))
		return
	}
	patchReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	patchReq.Header.Add("Content-Type", "application/vnd.api+json")

	patchResp, err := r.client.Do(patchReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace ssh patch request", fmt.Sprintf("Error executing request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(patchResp.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading workspace ssh patch response")
	}

	// Best-effort unmarshal
	ssh := &client.SshEntity{}
	_ = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), ssh)

	plan.ID = types.StringValue(fmt.Sprintf("%s,%s", plan.WorkspaceId.ValueString(), plan.SshId.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceSshResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	tflog.Info(ctx, "Workspace SSH Delete called", map[string]any{"time": time.Now().UTC().String(), "workspace": "unknown"})
	// we update workspace attribute later in function and will log actual workspace id before request
	var data WorkspaceSshResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Remove moduleSshKey by PATCHing workspace attributes to the API's expected delete marker
	// The API expects an empty string to remove the module SSH key
	payloadObj := map[string]any{
		"data": map[string]any{
			"type": "workspace",
			"id":   data.WorkspaceId.ValueString(),
			"attributes": map[string]any{
				"moduleSshKey": "",
			},
		},
	}

	payloadBytes, err := json.Marshal(payloadObj)
	if err != nil {
		resp.Diagnostics.AddError("Error marshaling workspace ssh delete payload", fmt.Sprintf("Error marshaling payload: %s", err))
		return
	}

	tflog.Info(ctx, "Workspace SSH delete payload", map[string]any{"body": string(payloadBytes)})

	reqDel, err := http.NewRequestWithContext(ctx, http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s", r.endpoint, data.OrganizationId.ValueString(), data.WorkspaceId.ValueString()), strings.NewReader(string(payloadBytes)))
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace ssh delete request", fmt.Sprintf("Error creating request: %s", err))
		return
	}
	reqDel.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	reqDel.Header.Add("Content-Type", "application/vnd.api+json")
	reqDel.Header.Add("Accept", "application/vnd.api+json")

	deleteResp, err := r.client.Do(reqDel)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace ssh delete request", fmt.Sprintf("Error executing request: %s", err))
		return
	}
	defer deleteResp.Body.Close()

	if deleteResp.StatusCode != http.StatusOK && deleteResp.StatusCode != http.StatusCreated && deleteResp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(deleteResp.Body)
		resp.Diagnostics.AddError("Error deleting workspace SSH link", fmt.Sprintf("Unexpected response status: %d, body: %s", deleteResp.StatusCode, string(body)))
		return
	}

	deleteBody, _ := io.ReadAll(deleteResp.Body)
	tflog.Info(ctx, "Workspace SSH delete response", map[string]any{"status": deleteResp.StatusCode, "body": string(deleteBody)})

	verifyReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s", r.endpoint, data.OrganizationId.ValueString(), data.WorkspaceId.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace verify request", fmt.Sprintf("Error creating request: %s", err))
		return
	}
	verifyReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	verifyReq.Header.Add("Accept", "application/vnd.api+json")

	verifyResp, err := r.client.Do(verifyReq)
	if err != nil {
		resp.Diagnostics.AddError("Error verifying workspace SSH deletion", fmt.Sprintf("Error executing request: %s", err))
		return
	}
	defer verifyResp.Body.Close()

	if verifyResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(verifyResp.Body)
		resp.Diagnostics.AddError("Error verifying workspace SSH deletion", fmt.Sprintf("Unexpected response status: %d, body: %s", verifyResp.StatusCode, string(body)))
		return
	}

	verifyBody, err := io.ReadAll(verifyResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading workspace verify response", fmt.Sprintf("Error reading response body: %s", err))
		return
	}

	tflog.Info(ctx, "Workspace SSH verify response", map[string]any{"status": verifyResp.StatusCode, "body": string(verifyBody)})

	var parsed map[string]interface{}
	if err := json.Unmarshal(verifyBody, &parsed); err != nil {
		resp.Diagnostics.AddError("Error parsing workspace verify response", fmt.Sprintf("Error parsing response body: %s", err))
		return
	}

	if data, ok := parsed["data"].(map[string]interface{}); ok {
		if attrs, ok := data["attributes"].(map[string]interface{}); ok {
			if v, ok := attrs["moduleSshKey"]; ok {
				switch val := v.(type) {
				case string:
					if val != "" {
						resp.Diagnostics.AddError("Workspace SSH deletion failed", fmt.Sprintf("moduleSshKey still present in workspace after delete: %v", val))
						return
					}
				case nil:
					// nil is acceptable (treat as absent)
				default:
					resp.Diagnostics.AddError("Workspace SSH deletion failed", fmt.Sprintf("Unexpected moduleSshKey value after delete: %v", val))
					return
				}
			}
		}
	}

	resp.State.RemoveResource(ctx)
}

func (r *WorkspaceSshResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, ",")
	if len(parts) != 3 {
		resp.Diagnostics.AddError("Unexpected Import Identifier", fmt.Sprintf("Expected import identifier with format: 'organization_id,workspace_id,ssh_id', Got: %q", req.ID))
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("ssh_id"), parts[2])...)
}
