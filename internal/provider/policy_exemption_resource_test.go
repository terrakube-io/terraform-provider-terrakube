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

func policyExemptionResourceSchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	r := &PolicyExemptionResource{}
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

func TestPolicyExemptionResource_Schema(t *testing.T) {
	ctx := context.Background()
	s, _ := policyExemptionResourceSchemaAndType(t, ctx)

	requiredAttrs := []string{"organization_id", "policy_set_id", "rule_id", "ticket_reference", "justification"}
	for _, attr := range requiredAttrs {
		a, ok := s.Attributes[attr]
		if !ok {
			t.Errorf("expected attribute %q to exist", attr)
		} else if !a.IsRequired() {
			t.Errorf("expected attribute %q to be required", attr)
		}
	}
}

func TestPolicyExemptionResource_Create(t *testing.T) {
	ctx := context.Background()
	s, objType := policyExemptionResourceSchemaAndType(t, ctx)

	const orgID = "org-100"
	var receivedBody string

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization/org-100/policyExemption", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)

		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"data":{"type":"policy_exemption","id":"ex-555","attributes":{
			"ruleId":"azure_apim_no_public_network",
			"ticketReference":"SEC-8842",
			"justification":"Approved integration gateway",
			"expiresAt":"2026-12-31T23:59:59Z"
		},"relationships":{
			"policySet":{"data":{"type":"policy_set","id":"ps-1"}},
			"workspace":{"data":{"type":"workspace","id":"ws-1"}}
		}}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &PolicyExemptionResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	planValue := buildObjectValue(objType, map[string]tftypes.Value{
		"organization_id":  tftypes.NewValue(tftypes.String, orgID),
		"policy_set_id":    tftypes.NewValue(tftypes.String, "ps-1"),
		"rule_id":          tftypes.NewValue(tftypes.String, "azure_apim_no_public_network"),
		"ticket_reference": tftypes.NewValue(tftypes.String, "SEC-8842"),
		"justification":    tftypes.NewValue(tftypes.String, "Approved integration gateway"),
		"expires_at":       tftypes.NewValue(tftypes.String, "2026-12-31T23:59:59Z"),
		"workspace_id":     tftypes.NewValue(tftypes.String, "ws-1"),
	})

	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValue}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

	r.Create(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state PolicyExemptionResourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("reading state: %v", diags)
	}

	if state.ID.ValueString() != "ex-555" {
		t.Errorf("ID = %q, want ex-555", state.ID.ValueString())
	}
	if state.RuleId.ValueString() != "azure_apim_no_public_network" {
		t.Errorf("RuleId = %q, want azure_apim_no_public_network", state.RuleId.ValueString())
	}
	if state.TicketReference.ValueString() != "SEC-8842" {
		t.Errorf("TicketReference = %q, want SEC-8842", state.TicketReference.ValueString())
	}
	if !contains(receivedBody, `"ticketReference":"SEC-8842"`) {
		t.Errorf("expected received body to have ticketReference, got: %s", receivedBody)
	}
}

func TestPolicyExemptionResource_Delete(t *testing.T) {
	ctx := context.Background()
	s, objType := policyExemptionResourceSchemaAndType(t, ctx)

	deleted := false
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/policy_exemption/ex-555", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Fatalf("unexpected method %s", r.Method)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &PolicyExemptionResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	stateValue := buildObjectValue(objType, map[string]tftypes.Value{
		"id":              tftypes.NewValue(tftypes.String, "ex-555"),
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
