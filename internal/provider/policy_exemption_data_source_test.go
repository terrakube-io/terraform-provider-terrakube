package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func policyExemptionDataSourceSchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	d := &PolicyExemptionDataSource{}
	var schemaResp datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics building schema: %v", schemaResp.Diagnostics)
	}

	objType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("expected schema type to be a tftypes.Object")
	}

	return schemaResp.Schema, objType
}

func TestPolicyExemptionDataSource_Read_ById(t *testing.T) {
	ctx := context.Background()
	s, objType := policyExemptionDataSourceSchemaAndType(t, ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"type":"organization","id":"org-1","attributes":{"name":"enterprise"}}]}`)
	})
	mux.HandleFunc("/api/v1/policy_exemption/ex-123", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":{
			"type":"policy_exemption",
			"id":"ex-123",
			"attributes":{
				"ruleId":"azure_apim_no_public_network",
				"ticketReference":"SEC-8842",
				"justification":"Integration gateway",
				"expiresAt":"2026-12-31T23:59:59Z"
			},
			"relationships":{
				"policySet":{"data":{"type":"policy_set","id":"ps-1"}},
				"workspace":{"data":{"type":"workspace","id":"ws-1"}}
			}
		}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	d := &PolicyExemptionDataSource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	configValue := buildObjectValue(objType, map[string]tftypes.Value{
		"organization": tftypes.NewValue(tftypes.String, "enterprise"),
		"id":           tftypes.NewValue(tftypes.String, "ex-123"),
	})

	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: configValue}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}

	d.Read(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state PolicyExemptionDataSourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("reading state: %v", diags)
	}

	if state.ID.ValueString() != "ex-123" {
		t.Errorf("ID = %q, want ex-123", state.ID.ValueString())
	}
	if state.RuleId.ValueString() != "azure_apim_no_public_network" {
		t.Errorf("RuleId = %q, want azure_apim_no_public_network", state.RuleId.ValueString())
	}
	if state.WorkspaceId.ValueString() != "ws-1" {
		t.Errorf("WorkspaceId = %q, want ws-1", state.WorkspaceId.ValueString())
	}
}

func TestPolicyExemptionDataSource_Read_ByRuleId(t *testing.T) {
	ctx := context.Background()
	s, objType := policyExemptionDataSourceSchemaAndType(t, ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"type":"organization","id":"org-1","attributes":{"name":"enterprise"}}]}`)
	})
	mux.HandleFunc("/api/v1/organization/org-1/policyExemption", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{
			"type":"policy_exemption",
			"id":"ex-rule-match",
			"attributes":{
				"ruleId":"azure_apim_no_public_network",
				"ticketReference":"SEC-9999",
				"justification":"Rule query justification"
			}
		}]}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	d := &PolicyExemptionDataSource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	configValue := buildObjectValue(objType, map[string]tftypes.Value{
		"organization": tftypes.NewValue(tftypes.String, "enterprise"),
		"rule_id":      tftypes.NewValue(tftypes.String, "azure_apim_no_public_network"),
	})

	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: configValue}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}

	d.Read(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state PolicyExemptionDataSourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("reading state: %v", diags)
	}

	if state.ID.ValueString() != "ex-rule-match" {
		t.Errorf("ID = %q, want ex-rule-match", state.ID.ValueString())
	}
	if state.TicketReference.ValueString() != "SEC-9999" {
		t.Errorf("TicketReference = %q, want SEC-9999", state.TicketReference.ValueString())
	}
}
