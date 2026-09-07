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

func policySetDataSourceSchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	d := &PolicySetDataSource{}
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

func TestPolicySetDataSource_Read(t *testing.T) {
	ctx := context.Background()
	s, objType := policySetDataSourceSchemaAndType(t, ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"type":"organization","id":"org-1","attributes":{"name":"enterprise"}}]}`)
	})
	mux.HandleFunc("/api/v1/organization/org-1/policySet", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{
			"type":"policy_set",
			"id":"ps-global",
			"attributes":{
				"name":"global-guardrails",
				"description":"Baseline CIS guardrails",
				"enforcementLevel":"HARD_MANDATORY",
				"global":true,
				"branch":"v2.0.0",
				"folder":"bundles/baseline"
			}
		}]}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	d := &PolicySetDataSource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	configValue := buildObjectValue(objType, map[string]tftypes.Value{
		"organization": tftypes.NewValue(tftypes.String, "enterprise"),
		"name":         tftypes.NewValue(tftypes.String, "global-guardrails"),
	})

	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: configValue}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}

	d.Read(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state PolicySetDataSourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("reading state: %v", diags)
	}

	if state.ID.ValueString() != "ps-global" {
		t.Errorf("ID = %q, want ps-global", state.ID.ValueString())
	}
	if state.EnforcementLevel.ValueString() != "hard_mandatory" {
		t.Errorf("EnforcementLevel = %q, want hard_mandatory", state.EnforcementLevel.ValueString())
	}
	if !state.Global.ValueBool() {
		t.Errorf("Global = false, want true")
	}
}
