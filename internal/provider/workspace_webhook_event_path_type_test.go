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

// These tests cover GH-168: the server has supported a per-event pathType
// (PATTERN or REGEX) since Terrakube 2.31.0, but the provider never sent or
// read it, so events created via Terraform were always REGEX and flipping the
// type in the UI produced invisible drift.

func TestWorkspaceWebhookEventResource_Schema_PathTypeDefaultsToRegex(t *testing.T) {
	ctx := context.Background()
	s, _ := webhookEventSchemaAndType(t, ctx)

	attr, ok := s.Attributes["path_type"]
	if !ok {
		t.Fatal("expected schema to define attribute \"path_type\"")
	}

	stringAttr, ok := attr.(schema.StringAttribute)
	if !ok {
		t.Fatalf("expected path_type to be a StringAttribute, got %T", attr)
	}

	if !stringAttr.Optional {
		t.Error("expected path_type to be Optional so existing configs keep working unchanged")
	}
	if !stringAttr.Computed {
		t.Error("expected path_type to be Computed so the default can be applied")
	}
	if stringAttr.Default == nil {
		t.Fatal("expected path_type to have a Default")
	}

	var defResp defaults.StringResponse
	stringAttr.Default.DefaultString(ctx, defaults.StringRequest{}, &defResp)
	if got := defResp.PlanValue.ValueString(); got != "REGEX" {
		t.Errorf("expected default path_type REGEX for backwards compatibility, got %q", got)
	}

	if len(stringAttr.Validators) == 0 {
		t.Fatal("expected path_type to have a OneOf validator")
	}
	desc := stringAttr.Validators[0].Description(ctx)
	for _, want := range []string{"PATTERN", "REGEX"} {
		if !strings.Contains(desc, want) {
			t.Errorf("expected validator to allow %q, validator description: %s", want, desc)
		}
	}
}

func TestWorkspaceWebhookEventResource_Create_SendsPathType(t *testing.T) {
	ctx := context.Background()
	s, objType := webhookEventSchemaAndType(t, ctx)

	const webhookID = "wh-pathtype-1"

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

	r := &WorkspaceWebhookEventResource{client: server.Client(), endpoint: server.URL, token: "test-token"}

	planValue := buildObjectValue(objType, map[string]tftypes.Value{
		"webhook_id":  tftypes.NewValue(tftypes.String, webhookID),
		"event":       tftypes.NewValue(tftypes.String, "PUSH"),
		"priority":    tftypes.NewValue(tftypes.Number, big.NewFloat(1)),
		"template_id": tftypes.NewValue(tftypes.String, "tmpl-1"),
		"path": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{
			tftypes.NewValue(tftypes.String, "terraform/*"),
			tftypes.NewValue(tftypes.String, "modules/**"),
		}),
		"path_type":           tftypes.NewValue(tftypes.String, "PATTERN"),
		"pr_workflow_enabled": tftypes.NewValue(tftypes.Bool, false),
		"pr_apply_enabled":    tftypes.NewValue(tftypes.Bool, false),
	})

	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValue}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

	r.Create(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics)
	}

	if !strings.Contains(string(capturedBody), `"pathType":"PATTERN"`) {
		t.Errorf("expected request body to include pathType:PATTERN, got: %s", capturedBody)
	}
	if !strings.Contains(string(capturedBody), `"path":"terraform/*,modules/**"`) {
		t.Errorf("expected request body to join path entries with a comma, got: %s", capturedBody)
	}

	var result WorkspaceWebhookEventResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}
	if got := result.PathType.ValueString(); got != "PATTERN" {
		t.Errorf("expected path_type PATTERN in state after create, got %q", got)
	}
}

func TestWorkspaceWebhookEventResource_Create_SendsDefaultRegexPathType(t *testing.T) {
	ctx := context.Background()
	s, objType := webhookEventSchemaAndType(t, ctx)

	const webhookID = "wh-pathtype-default"

	var capturedBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/webhook/"+webhookID, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"data":{"type":"webhook","id":%q,"attributes":{"name":"wh"},`+
			`"relationships":{"organization":{"data":{"type":"organization","id":"org-1"}},`+
			`"workspace":{"data":{"type":"workspace","id":"ws-1"}},"events":{"data":[]}}}}`, webhookID)
	})
	mux.HandleFunc("/api/v1/operations", func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		capturedBody = body
		fmt.Fprint(w, `{"atomic:results":[{"data":{"type":"webhook_event","id":"event-created-2"}}]}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &WorkspaceWebhookEventResource{client: server.Client(), endpoint: server.URL, token: "test-token"}

	// Terraform applies the schema Default before Create is called, so the
	// plan carries REGEX when the user did not set path_type.
	planValue := buildObjectValue(objType, map[string]tftypes.Value{
		"webhook_id":          tftypes.NewValue(tftypes.String, webhookID),
		"event":               tftypes.NewValue(tftypes.String, "PUSH"),
		"priority":            tftypes.NewValue(tftypes.Number, big.NewFloat(0)),
		"template_id":         tftypes.NewValue(tftypes.String, "tmpl-1"),
		"path_type":           tftypes.NewValue(tftypes.String, "REGEX"),
		"pr_workflow_enabled": tftypes.NewValue(tftypes.Bool, false),
		"pr_apply_enabled":    tftypes.NewValue(tftypes.Bool, false),
	})

	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValue}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

	r.Create(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics)
	}
	if !strings.Contains(string(capturedBody), `"pathType":"REGEX"`) {
		t.Errorf("expected request body to include pathType:REGEX by default, got: %s", capturedBody)
	}
}

func TestWorkspaceWebhookEventResource_Read_RoundTripsPathType(t *testing.T) {
	ctx := context.Background()
	s, objType := webhookEventSchemaAndType(t, ctx)

	const eventID = "event-read-pattern"

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/webhook_event/"+eventID, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"data":{"type":"webhook_event","id":%q,"attributes":{`+
			`"branch":"main","path":"terraform/*","pathType":"PATTERN","templateId":"tmpl-1",`+
			`"event":"PUSH","priority":5,"prWorkflowEnabled":false,"prApplyEnabled":false},`+
			`"relationships":{"webhook":{"data":{"type":"webhook","id":"wh-1"}}}}}`, eventID)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &WorkspaceWebhookEventResource{client: server.Client(), endpoint: server.URL, token: "test-token"}

	// Prior state says REGEX; the server says PATTERN (e.g. changed in the UI).
	stateValue := buildObjectValue(objType, map[string]tftypes.Value{
		"id":         tftypes.NewValue(tftypes.String, eventID),
		"webhook_id": tftypes.NewValue(tftypes.String, "wh-1"),
		"path_type":  tftypes.NewValue(tftypes.String, "REGEX"),
	})

	req := resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: stateValue}}
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: stateValue}}

	r.Read(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics)
	}

	var result WorkspaceWebhookEventResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}
	if got := result.PathType.ValueString(); got != "PATTERN" {
		t.Errorf("expected Read to detect drift to PATTERN, got %q", got)
	}
}

// TestWorkspaceWebhookEventResource_Read_MissingPathTypeIsRegex covers older
// Terrakube servers (< 2.31.0) that do not return pathType at all. The
// provider must normalize that to REGEX, matching both the server's own
// fallback and the schema default, or every plan would show a diff.
func TestWorkspaceWebhookEventResource_Read_MissingPathTypeIsRegex(t *testing.T) {
	ctx := context.Background()
	s, objType := webhookEventSchemaAndType(t, ctx)

	const eventID = "event-read-legacy"

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/webhook_event/"+eventID, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"data":{"type":"webhook_event","id":%q,"attributes":{`+
			`"branch":"main","path":".*\\.tf","templateId":"tmpl-1",`+
			`"event":"PUSH","priority":5,"prWorkflowEnabled":false,"prApplyEnabled":false},`+
			`"relationships":{"webhook":{"data":{"type":"webhook","id":"wh-1"}}}}}`, eventID)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &WorkspaceWebhookEventResource{client: server.Client(), endpoint: server.URL, token: "test-token"}

	stateValue := buildObjectValue(objType, map[string]tftypes.Value{
		"id":         tftypes.NewValue(tftypes.String, eventID),
		"webhook_id": tftypes.NewValue(tftypes.String, "wh-1"),
		"path_type":  tftypes.NewValue(tftypes.String, "REGEX"),
	})

	req := resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: stateValue}}
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: stateValue}}

	r.Read(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics)
	}

	var result WorkspaceWebhookEventResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}
	if result.PathType.IsNull() || result.PathType.IsUnknown() {
		t.Fatal("expected path_type to be a known value when the server omits pathType")
	}
	if got := result.PathType.ValueString(); got != "REGEX" {
		t.Errorf("expected missing pathType to normalize to REGEX, got %q", got)
	}
}

func TestWorkspaceWebhookEventResource_Update_RoundTripsPathType(t *testing.T) {
	ctx := context.Background()
	s, objType := webhookEventSchemaAndType(t, ctx)

	const (
		webhookID   = "wh-update-pathtype"
		workspaceID = "ws-1"
		orgID       = "org-1"
		eventID     = "event-update-1"
	)

	var capturedBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/webhook/"+webhookID, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"data":{"type":"webhook","id":%q,"attributes":{"name":"wh"},`+
			`"relationships":{"organization":{"data":{"type":"organization","id":%q}},`+
			`"workspace":{"data":{"type":"workspace","id":%q}},"events":{"data":[{"type":"webhook_event","id":%q}]}}}}`,
			webhookID, orgID, workspaceID, eventID)
	})
	mux.HandleFunc("/api/v1/workspace/"+workspaceID, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"data":{"type":"workspace","id":%q,"attributes":{"name":"ws"},`+
			`"relationships":{"organization":{"data":{"type":"organization","id":%q}}}}}`, workspaceID, orgID)
	})
	mux.HandleFunc("/api/v1/operations", func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		capturedBody = body
		fmt.Fprintf(w, `{"atomic:results":[{"data":{"type":"webhook_event","id":%q}}]}`, eventID)
	})
	mux.HandleFunc(fmt.Sprintf("/api/v1/organization/%s/workspace/%s/webhook/%s/events", orgID, workspaceID, webhookID),
		func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintf(w, `{"data":[{"type":"webhook_event","id":%q,"attributes":{`+
				`"branch":"main","path":"terraform/*","pathType":"PATTERN","templateId":"tmpl-1",`+
				`"event":"PUSH","priority":5,"prWorkflowEnabled":false,"prApplyEnabled":false},`+
				`"relationships":{"webhook":{"data":{"type":"webhook","id":%q}}}}]}`, eventID, webhookID)
		})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &WorkspaceWebhookEventResource{client: server.Client(), endpoint: server.URL, token: "test-token"}

	pathList := tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{
		tftypes.NewValue(tftypes.String, "terraform/*"),
	})
	branchList := tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{
		tftypes.NewValue(tftypes.String, "main"),
	})

	stateValue := buildObjectValue(objType, map[string]tftypes.Value{
		"id":                  tftypes.NewValue(tftypes.String, eventID),
		"webhook_id":          tftypes.NewValue(tftypes.String, webhookID),
		"event":               tftypes.NewValue(tftypes.String, "PUSH"),
		"priority":            tftypes.NewValue(tftypes.Number, big.NewFloat(5)),
		"template_id":         tftypes.NewValue(tftypes.String, "tmpl-1"),
		"path":                pathList,
		"branch":              branchList,
		"path_type":           tftypes.NewValue(tftypes.String, "REGEX"),
		"pr_workflow_enabled": tftypes.NewValue(tftypes.Bool, false),
		"pr_apply_enabled":    tftypes.NewValue(tftypes.Bool, false),
	})
	planValue := buildObjectValue(objType, map[string]tftypes.Value{
		"id":                  tftypes.NewValue(tftypes.String, eventID),
		"webhook_id":          tftypes.NewValue(tftypes.String, webhookID),
		"event":               tftypes.NewValue(tftypes.String, "PUSH"),
		"priority":            tftypes.NewValue(tftypes.Number, big.NewFloat(5)),
		"template_id":         tftypes.NewValue(tftypes.String, "tmpl-1"),
		"path":                pathList,
		"branch":              branchList,
		"path_type":           tftypes.NewValue(tftypes.String, "PATTERN"),
		"pr_workflow_enabled": tftypes.NewValue(tftypes.Bool, false),
		"pr_apply_enabled":    tftypes.NewValue(tftypes.Bool, false),
	})

	req := resource.UpdateRequest{
		State: tfsdk.State{Schema: s, Raw: stateValue},
		Plan:  tfsdk.Plan{Schema: s, Raw: planValue},
	}
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: stateValue}}

	r.Update(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Update returned diagnostics: %v", resp.Diagnostics)
	}

	if !strings.Contains(string(capturedBody), `"pathType":"PATTERN"`) {
		t.Errorf("expected update payload to include pathType:PATTERN, got: %s", capturedBody)
	}
	if !strings.Contains(string(capturedBody), `"op":"update"`) {
		t.Errorf("expected an in-place update operation (path_type must not force replacement), got: %s", capturedBody)
	}

	var result WorkspaceWebhookEventResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}
	if got := result.PathType.ValueString(); got != "PATTERN" {
		t.Errorf("expected path_type PATTERN in state after update, got %q", got)
	}
}
