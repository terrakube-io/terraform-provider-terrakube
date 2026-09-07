package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func policyAttachmentResourceSchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	r := &PolicyAttachmentResource{}
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

func TestPolicyAttachmentResource_Schema(t *testing.T) {
	ctx := context.Background()
	s, _ := policyAttachmentResourceSchemaAndType(t, ctx)

	if !s.Attributes["policy_set_id"].IsRequired() {
		t.Errorf("expected policy_set_id to be required")
	}
	if !s.Attributes["id"].IsComputed() {
		t.Errorf("expected id to be computed")
	}
}

func TestPolicyAttachmentResource_Create_RejectsInvalidScope(t *testing.T) {
	ctx := context.Background()
	s, objType := policyAttachmentResourceSchemaAndType(t, ctx)

	r := &PolicyAttachmentResource{}

	// Case 1: No scope provided
	planValueNoScope := buildObjectValue(objType, map[string]tftypes.Value{
		"policy_set_id": tftypes.NewValue(tftypes.String, "ps-1"),
	})
	resp1 := &resource.CreateResponse{State: tfsdk.State{Schema: s}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValueNoScope}}, resp1)
	if !resp1.Diagnostics.HasError() {
		t.Errorf("expected error when no scope specified")
	}

	// Case 2: Multiple scopes provided
	planValueMultipleScopes := buildObjectValue(objType, map[string]tftypes.Value{
		"policy_set_id": tftypes.NewValue(tftypes.String, "ps-1"),
		"workspace_id":  tftypes.NewValue(tftypes.String, "ws-1"),
		"project_id":    tftypes.NewValue(tftypes.String, "proj-1"),
	})
	resp2 := &resource.CreateResponse{State: tfsdk.State{Schema: s}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValueMultipleScopes}}, resp2)
	if !resp2.Diagnostics.HasError() {
		t.Errorf("expected error when multiple scopes specified")
	}
}

func TestPolicyAttachmentResource_Create_Success(t *testing.T) {
	ctx := context.Background()
	s, objType := policyAttachmentResourceSchemaAndType(t, ctx)

	var receivedBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/policy_set/ps-1/attachments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)

		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"data":{"type":"policy_attachment","id":"att-999","relationships":{
			"workspace":{"data":{"type":"workspace","id":"ws-101"}}
		}}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &PolicyAttachmentResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	planValue := buildObjectValue(objType, map[string]tftypes.Value{
		"policy_set_id": tftypes.NewValue(tftypes.String, "ps-1"),
		"workspace_id":  tftypes.NewValue(tftypes.String, "ws-101"),
	})

	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValue}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

	r.Create(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state PolicyAttachmentResourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("reading state: %v", diags)
	}

	if state.ID.ValueString() != "att-999" {
		t.Errorf("ID = %q, want att-999", state.ID.ValueString())
	}
	if state.WorkspaceId.ValueString() != "ws-101" {
		t.Errorf("WorkspaceId = %q, want ws-101", state.WorkspaceId.ValueString())
	}
	if !contains(receivedBody, `"type":"workspace","id":"ws-101"`) {
		t.Errorf("expected body to contain workspace relationship, got: %s", receivedBody)
	}
}

func TestPolicyAttachmentResource_Delete(t *testing.T) {
	ctx := context.Background()
	s, objType := policyAttachmentResourceSchemaAndType(t, ctx)

	deleted := false
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/policy_set/ps-1/attachments/att-999", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Fatalf("unexpected method %s", r.Method)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &PolicyAttachmentResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	stateValue := buildObjectValue(objType, map[string]tftypes.Value{
		"id":            tftypes.NewValue(tftypes.String, "att-999"),
		"policy_set_id": tftypes.NewValue(tftypes.String, "ps-1"),
	})

	req := resource.DeleteRequest{State: tfsdk.State{Schema: s, Raw: stateValue}}
	resp := &resource.DeleteResponse{}

	r.Delete(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	if !deleted {
		t.Errorf("expected DELETE request to have been called")
	}
}
