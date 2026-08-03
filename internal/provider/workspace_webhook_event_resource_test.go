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

// TestWorkspaceWebhookEventResource_Schema_WebhookIdRequiresReplace covers a
// real bug: webhook_id was a plain Required string with no plan modifiers, so
// Terraform planned a change to it as an in-place update. But the server
// cascade-deletes a webhook's events when the webhook itself is destroyed
// (e.g. the parent terrakube_workspace_webhook_v2 being tainted or otherwise
// replaced) — there is nothing left server-side to "update," so the planned
// update 404s with "Unknown identifier ... for events". webhook_id changing
// must force replacement of the event instead.
func TestWorkspaceWebhookEventResource_Schema_WebhookIdRequiresReplace(t *testing.T) {
	ctx := context.Background()
	s, _ := webhookEventSchemaAndType(t, ctx)

	attr, ok := s.Attributes["webhook_id"]
	if !ok {
		t.Fatal("expected schema to define attribute \"webhook_id\"")
	}

	stringAttr, ok := attr.(schema.StringAttribute)
	if !ok {
		t.Fatalf("expected webhook_id to be a StringAttribute, got %T", attr)
	}

	found := false
	for _, m := range stringAttr.PlanModifiers {
		if strings.Contains(m.Description(ctx), "destroy and recreate the resource") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected webhook_id to have a RequiresReplace plan modifier, so a change forces recreation instead of a doomed in-place update against a cascade-deleted event")
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

// TestWorkspaceWebhookEventResource_Create_ResolvesPriorityWhenUnset covers
// a real bug: priority is Optional+Computed with no Default and no
// UseStateForUnknown plan modifier, so Terraform leaves it unknown (not
// null) during Create when it's absent from config. Create() never wrote
// the value it actually sent back into state, so Terraform's protocol
// rejected the result as still-unknown-after-apply.
func TestWorkspaceWebhookEventResource_Create_ResolvesPriorityWhenUnset(t *testing.T) {
	ctx := context.Background()
	s, objType := webhookEventSchemaAndType(t, ctx)

	const webhookID = "wh-create-2"

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/webhook/"+webhookID, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"data":{"type":"webhook","id":%q,"attributes":{"name":"wh"},`+
			`"relationships":{"organization":{"data":{"type":"organization","id":"org-1"}},`+
			`"workspace":{"data":{"type":"workspace","id":"ws-1"}},"events":{"data":[]}}}}`, webhookID)
	})
	mux.HandleFunc("/api/v1/operations", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"atomic:results":[{"data":{"type":"webhook_event","id":"event-created-2"}}]}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &WorkspaceWebhookEventResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	planValue := buildObjectValue(objType, map[string]tftypes.Value{
		"webhook_id":  tftypes.NewValue(tftypes.String, webhookID),
		"event":       tftypes.NewValue(tftypes.String, "PUSH"),
		"template_id": tftypes.NewValue(tftypes.String, "tmpl-1"),
		// priority intentionally left unknown, matching what Terraform
		// actually produces for an Optional+Computed attribute that's
		// absent from config during Create (never null).
		"priority": tftypes.NewValue(tftypes.Number, tftypes.UnknownValue),
	})

	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValue}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

	r.Create(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics)
	}

	var result WorkspaceWebhookEventResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}

	if result.Priority.IsUnknown() {
		t.Fatal("expected priority to be resolved to a known value after create, but it is still unknown (Terraform's protocol rejects unknown values after apply)")
	}
	if result.Priority.ValueInt64() != 0 {
		t.Errorf("expected priority to resolve to 0 (the value actually sent), got %d", result.Priority.ValueInt64())
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

// TestWorkspaceWebhookEventResource_Read_DetectsDrift covers a real bug:
// Read() used to only check whether the event's ID still appeared in its
// parent webhook's events relationship, then write the untouched prior state
// straight back — it never actually re-fetched the event's own attributes.
// Any out-of-band change (a toggle flipped directly via the API, a priority
// changed by another apply) was invisible to `terraform plan`. Confirmed live
// against a real API: changing pr_apply_enabled directly in the database and
// running `terraform plan` reported no changes.
func TestWorkspaceWebhookEventResource_Read_DetectsDrift(t *testing.T) {
	ctx := context.Background()
	s, objType := webhookEventSchemaAndType(t, ctx)

	const (
		webhookID = "wh-read-drift-1"
		eventID   = "event-read-drift-1"
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/webhook_event/"+eventID, func(w http.ResponseWriter, _ *http.Request) {
		// The server reports values that differ from what's in prior state
		// below on every field, simulating drift introduced outside Terraform.
		fmt.Fprintf(w, `{"data":{"type":"webhook_event","id":%q,"attributes":{`+
			`"branch":"develop","path":"other/","templateId":"tmpl-2","event":"PUSH","priority":7,`+
			`"prWorkflowEnabled":false,"prApplyEnabled":false},`+
			`"relationships":{"webhook":{"data":{"type":"webhook","id":%q}}}}}`, eventID, webhookID)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &WorkspaceWebhookEventResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	staleState := buildObjectValue(objType, map[string]tftypes.Value{
		"id":                  tftypes.NewValue(tftypes.String, eventID),
		"webhook_id":          tftypes.NewValue(tftypes.String, webhookID),
		"event":               tftypes.NewValue(tftypes.String, "PULL_REQUEST"),
		"priority":            tftypes.NewValue(tftypes.Number, big.NewFloat(1)),
		"template_id":         tftypes.NewValue(tftypes.String, "tmpl-1"),
		"pr_workflow_enabled": tftypes.NewValue(tftypes.Bool, true),
		"pr_apply_enabled":    tftypes.NewValue(tftypes.Bool, true),
	})

	req := resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: staleState}}
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: staleState}}

	r.Read(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics)
	}

	var result WorkspaceWebhookEventResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}

	if result.Event.ValueString() != "PUSH" {
		t.Errorf("expected event refreshed to PUSH (the real server value), got %q", result.Event.ValueString())
	}
	if result.Priority.ValueInt64() != 7 {
		t.Errorf("expected priority refreshed to 7, got %d", result.Priority.ValueInt64())
	}
	if result.TemplateId.ValueString() != "tmpl-2" {
		t.Errorf("expected template_id refreshed to tmpl-2, got %q", result.TemplateId.ValueString())
	}
	if result.PrWorkflowEnabled.ValueBool() {
		t.Error("expected pr_workflow_enabled refreshed to false")
	}
	if result.PrApplyEnabled.ValueBool() {
		t.Error("expected pr_apply_enabled refreshed to false")
	}
}

func TestWorkspaceWebhookEventResource_Read_RemovesResourceWhenEventDeleted(t *testing.T) {
	ctx := context.Background()
	s, objType := webhookEventSchemaAndType(t, ctx)

	const eventID = "event-read-gone-1"

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/webhook_event/"+eventID, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"errors":[{"detail":"Unknown identifier"}]}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &WorkspaceWebhookEventResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	staleState := buildObjectValue(objType, map[string]tftypes.Value{
		"id":         tftypes.NewValue(tftypes.String, eventID),
		"webhook_id": tftypes.NewValue(tftypes.String, "wh-1"),
	})

	req := resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: staleState}}
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: staleState}}

	r.Read(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Error("expected state to be removed after a 404 from the API")
	}
}

// TestWorkspaceWebhookEventResource_Update_UsesPlanWebhookIdNotStaleState
// covers a real bug: Update() looked up the webhook via state.WebhookId (the
// prior value) instead of plan.WebhookId (the value being applied). When the
// parent terrakube_workspace_webhook_v2 is replaced (taint, or any other
// forced recreation), webhook_id changes and the old ID no longer exists —
// Update() then failed with "Webhook not found" even though the update
// itself was perfectly valid against the new webhook. Confirmed live: taint
// + apply on the parent webhook_v2 broke every dependent webhook_event.
func TestWorkspaceWebhookEventResource_Update_UsesPlanWebhookIdNotStaleState(t *testing.T) {
	ctx := context.Background()
	s, objType := webhookEventSchemaAndType(t, ctx)

	const (
		oldWebhookID = "wh-old-destroyed"
		newWebhookID = "wh-new-replacement"
		workspaceID  = "ws-taint-1"
		orgID        = "org-taint-1"
		eventID      = "event-taint-1"
	)

	mux := http.NewServeMux()
	// The old webhook was destroyed by the parent resource's replacement —
	// looking it up must not happen, and if it does, this 404s the test.
	mux.HandleFunc("/api/v1/webhook/"+oldWebhookID, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"errors":[{"detail":"Unknown identifier"}]}`)
	})
	mux.HandleFunc("/api/v1/webhook/"+newWebhookID, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"data":{"type":"webhook","id":%q,"attributes":{"name":"wh"},`+
			`"relationships":{"organization":{"data":{"type":"organization","id":%q}},`+
			`"workspace":{"data":{"type":"workspace","id":%q}},"events":{"data":[]}}}}`,
			newWebhookID, orgID, workspaceID)
	})
	mux.HandleFunc("/api/v1/workspace/"+workspaceID, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"data":{"type":"workspace","id":%q,"attributes":{"name":"ws"},`+
			`"relationships":{"organization":{"data":{"type":"organization","id":%q}}}}}`,
			workspaceID, orgID)
	})
	mux.HandleFunc("/api/v1/operations", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"atomic:results":[{"data":{"type":"webhook_event","id":%q}}]}`, eventID)
	})
	eventsPath := fmt.Sprintf("/api/v1/organization/%s/workspace/%s/webhook/%s/events", orgID, workspaceID, newWebhookID)
	mux.HandleFunc(eventsPath, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"data":[{"type":"webhook_event","id":%q,"attributes":{`+
			`"branch":"main","path":"/","templateId":"tmpl-1","event":"PULL_REQUEST","priority":1,`+
			`"prWorkflowEnabled":false,"prApplyEnabled":false},`+
			`"relationships":{"webhook":{"data":{"type":"webhook","id":%q}}}}]}`, eventID, newWebhookID)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &WorkspaceWebhookEventResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	base := map[string]tftypes.Value{
		"id":                  tftypes.NewValue(tftypes.String, eventID),
		"event":               tftypes.NewValue(tftypes.String, "PULL_REQUEST"),
		"priority":            tftypes.NewValue(tftypes.Number, big.NewFloat(1)),
		"template_id":         tftypes.NewValue(tftypes.String, "tmpl-1"),
		"pr_workflow_enabled": tftypes.NewValue(tftypes.Bool, false),
		"pr_apply_enabled":    tftypes.NewValue(tftypes.Bool, false),
	}

	stateOverrides := map[string]tftypes.Value{}
	for k, v := range base {
		stateOverrides[k] = v
	}
	stateOverrides["webhook_id"] = tftypes.NewValue(tftypes.String, oldWebhookID)

	planOverrides := map[string]tftypes.Value{}
	for k, v := range base {
		planOverrides[k] = v
	}
	planOverrides["webhook_id"] = tftypes.NewValue(tftypes.String, newWebhookID)

	req := resource.UpdateRequest{
		State: tfsdk.State{Schema: s, Raw: buildObjectValue(objType, stateOverrides)},
		Plan:  tfsdk.Plan{Schema: s, Raw: buildObjectValue(objType, planOverrides)},
	}
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s}}

	r.Update(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Update returned diagnostics (should have used plan.WebhookId, not the destroyed state.WebhookId): %v", resp.Diagnostics)
	}

	var result WorkspaceWebhookEventResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}
	if result.WebhookId.ValueString() != newWebhookID {
		t.Errorf("expected webhook_id in state to be the new webhook %q, got %q", newWebhookID, result.WebhookId.ValueString())
	}
}
