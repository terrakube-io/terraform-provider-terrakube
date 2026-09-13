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

func policySetResourceSchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	r := &PolicySetResource{}
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

func TestPolicySetResource_Schema(t *testing.T) {
	ctx := context.Background()
	s, _ := policySetResourceSchemaAndType(t, ctx)

	requiredAttrs := []string{"organization_id", "name"}
	for _, attr := range requiredAttrs {
		a, ok := s.Attributes[attr]
		if !ok {
			t.Errorf("expected attribute %q to exist", attr)
		} else if !a.IsRequired() {
			t.Errorf("expected attribute %q to be required", attr)
		}
	}

	computedAttrs := []string{"id", "enforcement_level", "global", "branch", "folder"}
	for _, attr := range computedAttrs {
		a, ok := s.Attributes[attr]
		if !ok {
			t.Errorf("expected attribute %q to exist", attr)
		} else if !a.IsComputed() {
			t.Errorf("expected attribute %q to be computed", attr)
		}
	}
}

func TestPolicySetResource_Create(t *testing.T) {
	ctx := context.Background()
	s, objType := policySetResourceSchemaAndType(t, ctx)

	const orgID = "org-100"
	var receivedBody string

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization/org-100/policySet", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)

		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"data":{"type":"policy_set","id":"ps-123","attributes":{
			"name":"blast-radius",
			"description":"Limits destruction",
			"enforcementLevel":"HARD_MANDATORY",
			"global":true,
			"branch":"main",
			"folder":"/",
			"opaVersion":"0.68.0"
		}}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &PolicySetResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	planValue := buildObjectValue(objType, map[string]tftypes.Value{
		"organization_id":   tftypes.NewValue(tftypes.String, orgID),
		"name":              tftypes.NewValue(tftypes.String, "blast-radius"),
		"description":       tftypes.NewValue(tftypes.String, "Limits destruction"),
		"enforcement_level": tftypes.NewValue(tftypes.String, "hard_mandatory"),
		"global":            tftypes.NewValue(tftypes.Bool, true),
		"branch":            tftypes.NewValue(tftypes.String, "main"),
		"folder":            tftypes.NewValue(tftypes.String, "/"),
		"opa_version":       tftypes.NewValue(tftypes.String, "0.68.0"),
	})

	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValue}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

	r.Create(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state PolicySetResourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("reading state: %v", diags)
	}

	if state.ID.ValueString() != "ps-123" {
		t.Errorf("ID = %q, want %q", state.ID.ValueString(), "ps-123")
	}
	if state.Name.ValueString() != "blast-radius" {
		t.Errorf("Name = %q, want %q", state.Name.ValueString(), "blast-radius")
	}
	if state.EnforcementLevel.ValueString() != "hard_mandatory" {
		t.Errorf("EnforcementLevel = %q, want %q", state.EnforcementLevel.ValueString(), "hard_mandatory")
	}
	if !state.Global.ValueBool() {
		t.Errorf("Global = false, want true")
	}
	if state.OpaVersion.ValueString() != "0.68.0" {
		t.Errorf("OpaVersion = %q, want 0.68.0", state.OpaVersion.ValueString())
	}
	if !contains(receivedBody, `"enforcementLevel":"HARD_MANDATORY"`) {
		t.Errorf("expected received body to normalize enforcementLevel to uppercase, got: %s", receivedBody)
	}
	if !contains(receivedBody, `"opaVersion":"0.68.0"`) {
		t.Errorf("expected received body to contain opaVersion, got: %s", receivedBody)
	}
}

func TestPolicySetResource_Read(t *testing.T) {
	ctx := context.Background()
	s, objType := policySetResourceSchemaAndType(t, ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/policy_set/ps-123", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method %s", r.Method)
		}
		fmt.Fprint(w, `{"data":{"type":"policy_set","id":"ps-123","attributes":{
			"name":"azure-tagging",
			"description":"Enforce tagging",
			"enforcementLevel":"SOFT_MANDATORY",
			"overrideTeam":"secops-approvers",
			"global":false,
			"branch":"v1.0.0",
			"folder":"/policies",
			"opaVersion":"0.68.0"
		}}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &PolicySetResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	stateValue := buildObjectValue(objType, map[string]tftypes.Value{
		"id":              tftypes.NewValue(tftypes.String, "ps-123"),
		"organization_id": tftypes.NewValue(tftypes.String, "org-100"),
	})

	req := resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: stateValue}}
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: s}}

	r.Read(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state PolicySetResourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("reading state: %v", diags)
	}

	if state.OpaVersion.ValueString() != "0.68.0" {
		t.Errorf("OpaVersion = %q, want 0.68.0", state.OpaVersion.ValueString())
	}

	if state.Name.ValueString() != "azure-tagging" {
		t.Errorf("Name = %q, want azure-tagging", state.Name.ValueString())
	}
	if state.EnforcementLevel.ValueString() != "soft_mandatory" {
		t.Errorf("EnforcementLevel = %q, want soft_mandatory", state.EnforcementLevel.ValueString())
	}
	if state.OverrideTeam.ValueString() != "secops-approvers" {
		t.Errorf("OverrideTeam = %q, want secops-approvers", state.OverrideTeam.ValueString())
	}
}

func TestPolicySetResource_Delete(t *testing.T) {
	ctx := context.Background()
	s, objType := policySetResourceSchemaAndType(t, ctx)

	deleted := false
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/policy_set/ps-123", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Fatalf("unexpected method %s", r.Method)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &PolicySetResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	stateValue := buildObjectValue(objType, map[string]tftypes.Value{
		"id":              tftypes.NewValue(tftypes.String, "ps-123"),
		"organization_id": tftypes.NewValue(tftypes.String, "org-100"),
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
