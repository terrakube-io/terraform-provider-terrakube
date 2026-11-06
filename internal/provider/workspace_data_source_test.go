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
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func workspaceDataSourceSchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	d := &WorkspaceDataSource{}
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

func TestWorkspaceDataSource_PolicyComplianceStatus(t *testing.T) {
	ctx := context.Background()
	s, objType := workspaceDataSourceSchemaAndType(t, ctx)

	attr, ok := s.Attributes["policy_compliance_status"]
	if !ok {
		t.Fatalf("expected policy_compliance_status attribute on workspace data source")
	}
	if !attr.IsComputed() {
		t.Errorf("expected policy_compliance_status to be computed")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"type":"organization","id":"org-1","attributes":{"name":"enterprise"}}]}`)
	})
	mux.HandleFunc("/api/v1/organization/org-1/workspace", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{
			"type":"workspace",
			"id":"ws-1",
			"attributes":{
				"name":"payments-core",
				"source":"https://github.com/org/payments",
				"branch":"main",
				"folder":"/",
				"terraformVersion":"1.15.8",
				"executionMode":"remote",
				"policyComplianceStatus":"COMPLIANT",
				"restrictApplyToConfiguredBranch":true,
				"requireApproveToApply":false
			}
		}]}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	d := &WorkspaceDataSource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	configValue := buildObjectValue(objType, map[string]tftypes.Value{
		"organization": tftypes.NewValue(tftypes.String, "enterprise"),
		"name":         tftypes.NewValue(tftypes.String, "payments-core"),
	})

	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: configValue}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}

	d.Read(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state WorkspaceDataSourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("reading state: %v", diags)
	}

	if !state.RestrictApplyToConfiguredBranch.ValueBool() || state.RequireApproveToApply.IsNull() || state.RequireApproveToApply.ValueBool() {
		t.Fatalf("unexpected workspace guards: %v / %v", state.RestrictApplyToConfiguredBranch, state.RequireApproveToApply)
	}
	if state.PolicyComplianceStatus.ValueString() != "COMPLIANT" {
		t.Errorf("PolicyComplianceStatus = %q, want COMPLIANT", state.PolicyComplianceStatus.ValueString())
	}
}

func TestWorkspaceDataSource_ApplyGuardCompatibility(t *testing.T) {
	for name, tc := range map[string]struct {
		attributes string
		want       types.Bool
	}{
		"enabled":   {`,"restrictApplyToConfiguredBranch":true,"requireApproveToApply":true`, types.BoolValue(true)},
		"disabled":  {`,"restrictApplyToConfiguredBranch":false,"requireApproveToApply":false`, types.BoolValue(false)},
		"older API": {``, types.BoolNull()},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s, objType := workspaceDataSourceSchemaAndType(t, ctx)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/organization" {
					_, _ = fmt.Fprint(w, `{"data":[{"type":"organization","id":"org-1","attributes":{"name":"enterprise"}}]}`)
					return
				}
				_, _ = fmt.Fprint(w, `{"data":[{"type":"workspace","id":"ws-1","attributes":{"name":"payments-core"`+tc.attributes+`}}]}`)
			}))
			defer server.Close()
			d := &WorkspaceDataSource{client: server.Client(), endpoint: server.URL, token: "test-token"}
			config := buildObjectValue(objType, map[string]tftypes.Value{
				"organization": tftypes.NewValue(tftypes.String, "enterprise"),
				"name":         tftypes.NewValue(tftypes.String, "payments-core"),
			})
			resp := datasource.ReadResponse{State: tfsdk.State{Schema: s}}
			d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: config}}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			var state WorkspaceDataSourceModel
			if diags := resp.State.Get(ctx, &state); diags.HasError() {
				t.Fatal(diags)
			}
			if !state.RestrictApplyToConfiguredBranch.Equal(tc.want) || !state.RequireApproveToApply.Equal(tc.want) {
				t.Fatalf("guard values = %v / %v, want %v", state.RestrictApplyToConfiguredBranch, state.RequireApproveToApply, tc.want)
			}
		})
	}
}
