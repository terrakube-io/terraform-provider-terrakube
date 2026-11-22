package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	terrakube "github.com/terrakube-io/terrakube-go"
)

func objectField(t *testing.T, object map[string]any, name string) map[string]any {
	t.Helper()

	value, ok := object[name]
	if !ok {
		t.Fatalf("expected object to contain %q: %#v", name, object)
	}
	field, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected %q to be an object, got %T", name, value)
	}
	return field
}

func TestWorkspaceRunTriggerResourceSchema(t *testing.T) {
	ctx := context.Background()
	r := &WorkspaceRunTriggerResource{}
	var response resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", response.Diagnostics)
	}

	for _, name := range []string{"source_workspace_id", "destination_workspace_id"} {
		attribute, ok := response.Schema.Attributes[name]
		if !ok {
			t.Fatalf("expected schema to define %q", name)
		}
		stringAttribute, ok := attribute.(schema.StringAttribute)
		if !ok || !stringAttribute.Required {
			t.Errorf("expected %q to be a required StringAttribute", name)
		}
		foundRequiresReplace := false
		for _, modifier := range stringAttribute.PlanModifiers {
			if strings.Contains(modifier.Description(ctx), "destroy and recreate the resource") {
				foundRequiresReplace = true
			}
		}
		if !foundRequiresReplace {
			t.Errorf("expected %q to require replacement when changed", name)
		}
	}

	enabled, ok := response.Schema.Attributes["enabled"].(schema.BoolAttribute)
	if !ok || !enabled.Optional || !enabled.Computed || enabled.Default == nil {
		t.Error("expected enabled to be optional, computed, and have a default")
	}
}

func TestWorkspaceRunTriggerRequestBodySendsNullTemplate(t *testing.T) {
	model := WorkspaceRunTriggerResourceModel{
		SourceWorkspaceID:      types.StringValue("source-1"),
		DestinationWorkspaceID: types.StringValue("destination-1"),
		TemplateID:             types.StringNull(),
		Enabled:                types.BoolValue(true),
	}
	body, err := workspaceRunTriggerUpdateRequestBody(model, "trigger-1")
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshalling request body: %v", err)
	}
	data := objectField(t, payload, "data")
	if data["type"] != "runTrigger" || data["id"] != "trigger-1" {
		t.Errorf("unexpected JSON:API resource identifier: %#v", data)
	}
	relationships := objectField(t, data, "relationships")
	if objectField(t, relationships, "template")["data"] != nil {
		t.Errorf("expected omitted template_id to be encoded as a null relationship, got %s", body)
	}
	if _, exists := relationships["sourceWorkspace"]; exists {
		t.Errorf("PATCH must not resend immutable sourceWorkspace, got %s", body)
	}
	if _, exists := relationships["destinationWorkspace"]; exists {
		t.Errorf("PATCH must not resend immutable destinationWorkspace, got %s", body)
	}
}

func TestWorkspaceRunTriggerCreateRequestBodyIncludesWorkspaceRelationships(t *testing.T) {
	model := WorkspaceRunTriggerResourceModel{
		SourceWorkspaceID:      types.StringValue("source-1"),
		DestinationWorkspaceID: types.StringValue("destination-1"),
		Enabled:                types.BoolValue(true),
	}
	body, err := workspaceRunTriggerCreateRequestBody(model)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshalling request body: %v", err)
	}
	relationships := objectField(t, objectField(t, payload, "data"), "relationships")
	for relationship, id := range map[string]string{"sourceWorkspace": "source-1", "destinationWorkspace": "destination-1"} {
		identifier := objectField(t, objectField(t, relationships, relationship), "data")
		if identifier["type"] != "workspace" || identifier["id"] != id {
			t.Errorf("unexpected %s relationship: %#v", relationship, identifier)
		}
	}
}

// sentRequestBody runs a resource operation against a test server that answers every request
// with response, and returns the body of the first request sent with method.
func sentRequestBody(method, response string, run func(client *terrakube.Client) diag.Diagnostics) ([]byte, error) {
	var mu sync.Mutex
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		if r.Method == method && body == nil {
			body = raw
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/vnd.api+json")
		_, _ = io.WriteString(w, response)
	}))
	defer server.Close()

	if diags := run(newTestClient(server.Client(), server.URL)); diags.HasError() {
		return nil, fmt.Errorf("unexpected diagnostics: %v", diags)
	}
	mu.Lock()
	defer mu.Unlock()
	if body == nil {
		return nil, fmt.Errorf("no %s request was sent", method)
	}
	return body, nil
}

// modelPlan returns a plan of the resource's schema that holds model.
func modelPlan(ctx context.Context, r resource.Resource, model any) (tfsdk.Plan, diag.Diagnostics) {
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	plan := tfsdk.Plan{Schema: schemaResp.Schema}
	diags := plan.Set(ctx, model)
	return plan, diags
}

const workspaceRunTriggerTestResponse = `{"data":{"type":"runTrigger","id":"trigger-1","attributes":{"enabled":true}}}`

// workspaceRunTriggerCreateRequestBody returns the body Create sends for model.
func workspaceRunTriggerCreateRequestBody(model WorkspaceRunTriggerResourceModel) ([]byte, error) {
	return sentRequestBody(http.MethodPost, workspaceRunTriggerTestResponse, func(client *terrakube.Client) diag.Diagnostics {
		ctx := context.Background()
		r := &WorkspaceRunTriggerResource{client: client}
		model.ID = types.StringUnknown()
		plan, diags := modelPlan(ctx, r, model)
		if diags.HasError() {
			return diags
		}
		resp := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
		r.Create(ctx, resource.CreateRequest{Plan: plan}, &resp)
		return resp.Diagnostics
	})
}

// workspaceRunTriggerUpdateRequestBody returns the body Update sends for model on the run trigger id.
func workspaceRunTriggerUpdateRequestBody(model WorkspaceRunTriggerResourceModel, id string) ([]byte, error) {
	return sentRequestBody(http.MethodPatch, workspaceRunTriggerTestResponse, func(client *terrakube.Client) diag.Diagnostics {
		ctx := context.Background()
		r := &WorkspaceRunTriggerResource{client: client}
		model.ID = types.StringValue(id)
		plan, diags := modelPlan(ctx, r, model)
		if diags.HasError() {
			return diags
		}
		state := tfsdk.State(plan)
		resp := resource.UpdateResponse{State: state}
		r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state, Config: tfsdk.Config(plan)}, &resp)
		return resp.Diagnostics
	})
}
