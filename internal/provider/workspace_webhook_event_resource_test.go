package provider

import (
	"context"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/defaults"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// webhookEventSchemaAndType returns the resource's schema along with the
// tftypes.Object type it maps to, so tests can build raw plan/state values
// without hand-maintaining a duplicate of the attribute list.
func webhookEventSchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	r := &WorkspaceWebhookEventResource{}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics building schema: %v", schemaResp.Diagnostics)
	}

	objType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("expected schema type to be a tftypes.Object")
	}

	return schemaResp.Schema, objType
}

// buildObjectValue fills every attribute of objType with a null value, then
// overrides the ones provided. This keeps each test focused on the
// attributes it actually cares about.
func buildObjectValue(objType tftypes.Object, overrides map[string]tftypes.Value) tftypes.Value {
	values := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, attrType := range objType.AttributeTypes {
		if v, ok := overrides[name]; ok {
			values[name] = v
		} else {
			values[name] = tftypes.NewValue(attrType, nil)
		}
	}
	return tftypes.NewValue(objType, values)
}

func TestWorkspaceWebhookEventResource_Schema_PrToggles(t *testing.T) {
	ctx := context.Background()
	s, _ := webhookEventSchemaAndType(t, ctx)

	for _, name := range []string{"pr_workflow_enabled", "pr_apply_enabled"} {
		attr, ok := s.Attributes[name]
		if !ok {
			t.Fatalf("expected schema to define attribute %q", name)
		}

		boolAttr, ok := attr.(schema.BoolAttribute)
		if !ok {
			t.Fatalf("expected %q to be a BoolAttribute, got %T", name, attr)
		}

		if !boolAttr.Optional {
			t.Errorf("%q: expected Optional to be true", name)
		}
		if !boolAttr.Computed {
			t.Errorf("%q: expected Computed to be true", name)
		}
		if boolAttr.Default == nil {
			t.Fatalf("%q: expected a Default to be set", name)
		}

		var defResp defaults.BoolResponse
		boolAttr.Default.DefaultBool(ctx, defaults.BoolRequest{}, &defResp)
		if defResp.PlanValue.ValueBool() {
			t.Errorf("%q: expected default value false, got %v", name, defResp.PlanValue)
		}
	}
}

func TestWorkspaceWebhookEventResource_Create_SendsPrToggles(t *testing.T) {
	ctx := context.Background()
	s, objType := webhookEventSchemaAndType(t, ctx)

	const webhookID = "wh-create-1"

	var capturedBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/webhook/"+webhookID, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"data":{"type":"webhook","id":%q,"attributes":{"name":"wh"},`+
			`"relationships":{"organization":{"data":{"type":"organization","id":"org-1"}},`+
			`"workspace":{"data":{"type":"workspace","id":"ws-1"}},"events":{"data":[]}}}}`, webhookID)
	})
	mux.HandleFunc("/api/v1/operations", func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		capturedBody = body
		fmt.Fprint(w, `{"atomic:results":[{"data":{"type":"webhook_event","id":"event-created-1"}}]}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &WorkspaceWebhookEventResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	planValue := buildObjectValue(objType, map[string]tftypes.Value{
		"webhook_id":          tftypes.NewValue(tftypes.String, webhookID),
		"event":               tftypes.NewValue(tftypes.String, "PULL_REQUEST"),
		"priority":            tftypes.NewValue(tftypes.Number, big.NewFloat(1)),
		"template_id":         tftypes.NewValue(tftypes.String, "tmpl-1"),
		"pr_workflow_enabled": tftypes.NewValue(tftypes.Bool, true),
		"pr_apply_enabled":    tftypes.NewValue(tftypes.Bool, true),
	})

	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValue}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

	r.Create(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics)
	}

	if !strings.Contains(string(capturedBody), `"prWorkflowEnabled":true`) {
		t.Errorf("expected request body to include prWorkflowEnabled:true, got: %s", capturedBody)
	}
	if !strings.Contains(string(capturedBody), `"prApplyEnabled":true`) {
		t.Errorf("expected request body to include prApplyEnabled:true, got: %s", capturedBody)
	}

	var result WorkspaceWebhookEventResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}

	if !result.PrWorkflowEnabled.ValueBool() {
		t.Error("expected pr_workflow_enabled to remain true in state after create")
	}
	if !result.PrApplyEnabled.ValueBool() {
		t.Error("expected pr_apply_enabled to remain true in state after create")
	}
}

func TestWorkspaceWebhookEventResource_Update_RoundTripsPrToggles(t *testing.T) {
	ctx := context.Background()
	s, objType := webhookEventSchemaAndType(t, ctx)

	const (
		webhookID   = "wh-update-1"
		workspaceID = "ws-update-1"
		orgID       = "org-update-1"
		eventID     = "event-update-1"
	)

	var capturedBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/webhook/"+webhookID, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"data":{"type":"webhook","id":%q,"attributes":{"name":"wh"},`+
			`"relationships":{"organization":{"data":{"type":"organization","id":%q}},`+
			`"workspace":{"data":{"type":"workspace","id":%q}},"events":{"data":[]}}}}`,
			webhookID, orgID, workspaceID)
	})
	mux.HandleFunc("/api/v1/workspace/"+workspaceID, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"data":{"type":"workspace","id":%q,"attributes":{"name":"ws"},`+
			`"relationships":{"organization":{"data":{"type":"organization","id":%q}}}}}`,
			workspaceID, orgID)
	})
	mux.HandleFunc("/api/v1/operations", func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		capturedBody = body
		fmt.Fprintf(w, `{"atomic:results":[{"data":{"type":"webhook_event","id":%q}}]}`, eventID)
	})
	eventsPath := fmt.Sprintf("/api/v1/organization/%s/workspace/%s/webhook/%s/events", orgID, workspaceID, webhookID)
	mux.HandleFunc(eventsPath, func(w http.ResponseWriter, _ *http.Request) {
		// The server reports the toggles the way they'd look right after this
		// update was applied: workflow on, apply still off. Using different
		// values for the two fields catches a mix-up between them.
		fmt.Fprintf(w, `{"data":[{"type":"webhook_event","id":%q,"attributes":{`+
			`"branch":"main","path":"/","templateId":"tmpl-1","event":"PULL_REQUEST","priority":1,`+
			`"createdBy":"x","createdDate":"x","updatedBy":"x","updatedDate":"x",`+
			`"prWorkflowEnabled":true,"prApplyEnabled":false},`+
			`"relationships":{"webhook":{"data":{"type":"webhook","id":%q}}}}]}`, eventID, webhookID)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &WorkspaceWebhookEventResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	base := map[string]tftypes.Value{
		"id":          tftypes.NewValue(tftypes.String, eventID),
		"webhook_id":  tftypes.NewValue(tftypes.String, webhookID),
		"event":       tftypes.NewValue(tftypes.String, "PULL_REQUEST"),
		"priority":    tftypes.NewValue(tftypes.Number, big.NewFloat(1)),
		"template_id": tftypes.NewValue(tftypes.String, "tmpl-1"),
	}

	stateOverrides := map[string]tftypes.Value{}
	for k, v := range base {
		stateOverrides[k] = v
	}
	stateOverrides["pr_workflow_enabled"] = tftypes.NewValue(tftypes.Bool, false)
	stateOverrides["pr_apply_enabled"] = tftypes.NewValue(tftypes.Bool, false)

	// Plan asks to turn pr_workflow_enabled on while leaving pr_apply_enabled
	// off, matching the UI's gating rule that apply requires workflow first.
	planOverrides := map[string]tftypes.Value{}
	for k, v := range base {
		planOverrides[k] = v
	}
	planOverrides["pr_workflow_enabled"] = tftypes.NewValue(tftypes.Bool, true)
	planOverrides["pr_apply_enabled"] = tftypes.NewValue(tftypes.Bool, false)

	req := resource.UpdateRequest{
		State: tfsdk.State{Schema: s, Raw: buildObjectValue(objType, stateOverrides)},
		Plan:  tfsdk.Plan{Schema: s, Raw: buildObjectValue(objType, planOverrides)},
	}
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s}}

	r.Update(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Update returned diagnostics: %v", resp.Diagnostics)
	}

	if !strings.Contains(string(capturedBody), `"prWorkflowEnabled":true`) {
		t.Errorf("expected request body to send the planned prWorkflowEnabled:true, got: %s", capturedBody)
	}
	if !strings.Contains(string(capturedBody), `"prApplyEnabled":false`) {
		t.Errorf("expected request body to send the planned prApplyEnabled:false, got: %s", capturedBody)
	}

	var result WorkspaceWebhookEventResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}

	if !result.PrWorkflowEnabled.ValueBool() {
		t.Error("expected pr_workflow_enabled read back from the API to be true")
	}
	if result.PrApplyEnabled.ValueBool() {
		t.Error("expected pr_apply_enabled read back from the API to be false")
	}
}
