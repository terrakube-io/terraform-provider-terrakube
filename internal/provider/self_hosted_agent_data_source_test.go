package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func readSelfHostedAgentDataSource(t *testing.T, name string, handler http.Handler) *datasource.ReadResponse {
	t.Helper()
	ctx := context.Background()
	d := &SelfHostedAgentDataSource{}
	var schemaResp datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics building schema: %v", schemaResp.Diagnostics)
	}
	objType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("expected schema type to be a tftypes.Object")
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	d.client = server.Client()
	d.endpoint = server.URL
	d.token = "test-token"

	configValue := buildObjectValue(objType, map[string]tftypes.Value{
		"name":            tftypes.NewValue(tftypes.String, name),
		"organization_id": tftypes.NewValue(tftypes.String, "org-1"),
	})
	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: configValue}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
	d.Read(ctx, req, resp)
	return resp
}

func TestSelfHostedAgentDataSource_Read(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/organization/org-1/agent" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("filter[agent]"); got != `name=="build agent & more"` {
			t.Errorf("filter = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/vnd.api+json" {
			t.Errorf("Content-Type = %q", got)
		}
		fmt.Fprint(w, `{"data":[
			{"type":"agent","id":"other","attributes":{"name":"other","description":"wrong","url":"https://other.example.com"}},
			{"type":"agent","id":"agent-1","attributes":{"name":"build agent & more","description":"Runs builds","url":"https://agent.example.com"}}
		]}`)
	})

	resp := readSelfHostedAgentDataSource(t, "build agent & more", handler)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	var state SelfHostedAgentDataSourceModel
	if diags := resp.State.Get(context.Background(), &state); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}
	if state.ID.ValueString() != "agent-1" || state.OrganizationId.ValueString() != "org-1" ||
		state.Name.ValueString() != "build agent & more" || state.Description.ValueString() != "Runs builds" ||
		state.Url.ValueString() != "https://agent.example.com" {
		t.Errorf("unexpected state: %+v", state)
	}
}

func TestSelfHostedAgentDataSource_ReadErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		summary string
	}{
		{"not found", http.StatusOK, `{"data":[]}`, "Self-hosted agent not found"},
		{"unmatched", http.StatusOK, `{"data":[{"type":"agent","id":"1","attributes":{"name":"other"}}]}`, "Self-hosted agent not found"},
		{"duplicate", http.StatusOK, `{"data":[{"type":"agent","id":"1","attributes":{"name":"agent"}},{"type":"agent","id":"2","attributes":{"name":"agent"}}]}`, "Multiple self-hosted agents found"},
		{"api error", http.StatusForbidden, `{"errors":[{"detail":"forbidden"}]}`, "Error reading self-hosted agent: 403 Forbidden"},
		{"invalid payload", http.StatusOK, `not json`, "Unable to unmarshal self-hosted agent payload"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			})
			resp := readSelfHostedAgentDataSource(t, "agent", handler)
			if !resp.Diagnostics.HasError() {
				t.Fatal("expected an error diagnostic")
			}
			if got := resp.Diagnostics.Errors()[0].Summary(); got != test.summary {
				t.Errorf("diagnostic = %q, want %q", got, test.summary)
			}
		})
	}
}
