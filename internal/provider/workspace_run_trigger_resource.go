package provider

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ resource.Resource = &WorkspaceRunTriggerResource{}
var _ resource.ResourceWithImportState = &WorkspaceRunTriggerResource{}

type WorkspaceRunTriggerResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type WorkspaceRunTriggerResourceModel struct {
	ID                     types.String `tfsdk:"id"`
	SourceWorkspaceID      types.String `tfsdk:"source_workspace_id"`
	DestinationWorkspaceID types.String `tfsdk:"destination_workspace_id"`
	TemplateID             types.String `tfsdk:"template_id"`
	Enabled                types.Bool   `tfsdk:"enabled"`
}

// workspaceRunTriggerPayload is intentionally explicit JSON rather than using
// jsonapi.MarshalPayload: JSON:API distinguishes an omitted relationship from
// {"data": null}, and PATCH needs the latter to remove template_id.
type workspaceRunTriggerPayload struct {
	Data workspaceRunTriggerData `json:"data"`
}

type workspaceRunTriggerData struct {
	Type          string                           `json:"type"`
	ID            string                           `json:"id,omitempty"`
	Attributes    workspaceRunTriggerAttributes    `json:"attributes"`
	Relationships workspaceRunTriggerRelationships `json:"relationships"`
}

type workspaceRunTriggerAttributes struct {
	Enabled bool `json:"enabled"`
}

type workspaceRunTriggerRelationships struct {
	SourceWorkspace      workspaceRunTriggerRelationship `json:"sourceWorkspace"`
	DestinationWorkspace workspaceRunTriggerRelationship `json:"destinationWorkspace"`
	Template             workspaceRunTriggerRelationship `json:"template"`
}

type workspaceRunTriggerRelationship struct {
	Data *workspaceRunTriggerResourceIdentifier `json:"data"`
}

type workspaceRunTriggerResourceIdentifier struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// workspaceRunTriggerUpdatePayload deliberately excludes sourceWorkspace and
// destinationWorkspace. They are ForceNew in Terraform and superuser-only on
// the API, so resending them during an enabled/template PATCH is unnecessary
// and can cause an authorization failure on stricter servers.
type workspaceRunTriggerUpdatePayload struct {
	Data struct {
		Type          string                        `json:"type"`
		ID            string                        `json:"id"`
		Attributes    workspaceRunTriggerAttributes `json:"attributes"`
		Relationships struct {
			Template workspaceRunTriggerRelationship `json:"template"`
		} `json:"relationships"`
	} `json:"data"`
}

func NewWorkspaceRunTriggerResource() resource.Resource { return &WorkspaceRunTriggerResource{} }

func (r *WorkspaceRunTriggerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_run_trigger"
}

func (r *WorkspaceRunTriggerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Creates a directed run trigger: a successful apply in the source workspace queues a run in the destination workspace. Requires Terrakube 2.34.0 or later.",
		Attributes: map[string]schema.Attribute{
			"id":                       schema.StringAttribute{Computed: true, Description: "Run trigger ID (UUID)", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"source_workspace_id":      schema.StringAttribute{Required: true, Description: "ID of the upstream workspace whose successful apply fires the trigger. Changing this forces a new resource.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"destination_workspace_id": schema.StringAttribute{Required: true, Description: "ID of the downstream workspace that receives the triggered run. Changing this forces a new resource.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"template_id":              schema.StringAttribute{Optional: true, Description: "Optional template ID for the triggered run. When unset, the destination workspace default template is used."},
			"enabled":                  schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true), Description: "Whether this trigger is active."},
		},
	}
}

func (r *WorkspaceRunTriggerResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Workspace Run Trigger Resource Configure Type", fmt.Sprintf("Expected *TerrakubeConnectionData, got: %T. Please report this issue to the provider developers.", req.ProviderData))
		return
	}
	if providerData.InsecureHttpClient {
		if transport, ok := http.DefaultTransport.(*http.Transport); ok {
			customTransport := transport.Clone()
			customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
			r.client = &http.Client{Transport: customTransport}
		} else {
			r.client = &http.Client{}
		}
	} else {
		r.client = &http.Client{}
	}
	r.endpoint, r.token = providerData.Endpoint, providerData.Token
	tflog.Debug(ctx, "Configuring Workspace Run Trigger resource")
}

func (r *WorkspaceRunTriggerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceRunTriggerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, err := workspaceRunTriggerCreateRequestBody(plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal run trigger payload", err.Error())
		return
	}
	trigger, found, err := r.request(ctx, http.MethodPost, "/api/v1/runTrigger", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace run trigger", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Error creating workspace run trigger", "Terrakube did not create a run trigger. Confirm that the endpoint is available (run triggers require Terrakube 2.34.0 or later).")
		return
	}
	setWorkspaceRunTriggerState(&plan, trigger)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceRunTriggerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceRunTriggerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	trigger, found, err := r.request(ctx, http.MethodGet, "/api/v1/runTrigger/"+state.ID.ValueString(), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error reading workspace run trigger", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	setWorkspaceRunTriggerState(&state, trigger)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *WorkspaceRunTriggerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var state, plan WorkspaceRunTriggerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, err := workspaceRunTriggerUpdateRequestBody(plan, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal run trigger payload", err.Error())
		return
	}
	_, found, err := r.request(ctx, http.MethodPatch, "/api/v1/runTrigger/"+state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating workspace run trigger", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	// Elide deployments may return either the updated entity or 204 No Content
	// for PATCH. Read after a successful update so state is reliable in both
	// cases and reflects any server-side normalization.
	trigger, found, err := r.request(ctx, http.MethodGet, "/api/v1/runTrigger/"+state.ID.ValueString(), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error reading updated workspace run trigger", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	setWorkspaceRunTriggerState(&plan, trigger)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceRunTriggerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state WorkspaceRunTriggerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, _, err := r.request(ctx, http.MethodDelete, "/api/v1/runTrigger/"+state.ID.ValueString(), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error deleting workspace run trigger", err.Error())
	}
}

func (r *WorkspaceRunTriggerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func workspaceRunTriggerCreateRequestBody(model WorkspaceRunTriggerResourceModel) ([]byte, error) {
	payload := workspaceRunTriggerPayload{Data: workspaceRunTriggerData{
		Type: "runTrigger", Attributes: workspaceRunTriggerAttributes{Enabled: model.Enabled.ValueBool()},
		Relationships: workspaceRunTriggerRelationships{
			SourceWorkspace:      workspaceRunTriggerRelationship{Data: &workspaceRunTriggerResourceIdentifier{Type: "workspace", ID: model.SourceWorkspaceID.ValueString()}},
			DestinationWorkspace: workspaceRunTriggerRelationship{Data: &workspaceRunTriggerResourceIdentifier{Type: "workspace", ID: model.DestinationWorkspaceID.ValueString()}},
		},
	}}
	if !model.TemplateID.IsNull() && !model.TemplateID.IsUnknown() && model.TemplateID.ValueString() != "" {
		payload.Data.Relationships.Template.Data = &workspaceRunTriggerResourceIdentifier{Type: "template", ID: model.TemplateID.ValueString()}
	}
	return json.Marshal(payload)
}

func workspaceRunTriggerUpdateRequestBody(model WorkspaceRunTriggerResourceModel, id string) ([]byte, error) {
	var payload workspaceRunTriggerUpdatePayload
	payload.Data.Type = "runTrigger"
	payload.Data.ID = id
	payload.Data.Attributes.Enabled = model.Enabled.ValueBool()
	if !model.TemplateID.IsNull() && !model.TemplateID.IsUnknown() && model.TemplateID.ValueString() != "" {
		payload.Data.Relationships.Template.Data = &workspaceRunTriggerResourceIdentifier{Type: "template", ID: model.TemplateID.ValueString()}
	}
	return json.Marshal(payload)
}

func (r *WorkspaceRunTriggerResource) request(ctx context.Context, method, resourcePath string, body []byte) (workspaceRunTriggerData, bool, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.endpoint+resourcePath, reader)
	if err != nil {
		return workspaceRunTriggerData{}, false, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Accept", "application/vnd.api+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/vnd.api+json")
	}
	response, err := r.client.Do(req)
	if err != nil {
		return workspaceRunTriggerData{}, false, fmt.Errorf("executing request: %w", err)
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(response.Body)
	if readErr != nil {
		return workspaceRunTriggerData{}, false, fmt.Errorf("reading response: %w", readErr)
	}
	if response.StatusCode == http.StatusNotFound {
		return workspaceRunTriggerData{}, false, nil
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return workspaceRunTriggerData{}, false, fmt.Errorf("terrakube returned %s: %s", response.Status, strings.TrimSpace(string(responseBody)))
	}
	if method == http.MethodDelete || len(responseBody) == 0 {
		return workspaceRunTriggerData{}, true, nil
	}
	var payload workspaceRunTriggerPayload
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return workspaceRunTriggerData{}, false, fmt.Errorf("decoding JSON:API response: %w", err)
	}
	return payload.Data, true, nil
}

func setWorkspaceRunTriggerState(model *WorkspaceRunTriggerResourceModel, trigger workspaceRunTriggerData) {
	model.ID = types.StringValue(trigger.ID)
	model.Enabled = types.BoolValue(trigger.Attributes.Enabled)
	if trigger.Relationships.SourceWorkspace.Data != nil {
		model.SourceWorkspaceID = types.StringValue(trigger.Relationships.SourceWorkspace.Data.ID)
	}
	if trigger.Relationships.DestinationWorkspace.Data != nil {
		model.DestinationWorkspaceID = types.StringValue(trigger.Relationships.DestinationWorkspace.Data.ID)
	}
	if trigger.Relationships.Template.Data == nil {
		model.TemplateID = types.StringNull()
	} else {
		model.TemplateID = types.StringValue(trigger.Relationships.Template.Data.ID)
	}
}
