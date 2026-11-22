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

// runTriggerDestinationPath is where the data source lists the destination workspace's run
// triggers, after reading the workspace for its organization.
const runTriggerDestinationPath = "/api/v1/organization/org-1/workspace/ws-destination/runTriggers"

// readWorkspaceRunTriggerDataSource runs Read with handler serving the run trigger list. The
// destination workspace, read first for its organization, is served by workspaceHandler, or as
// workspace ws-destination of organization org-1 when workspaceHandler is nil.
func readWorkspaceRunTriggerDataSource(t *testing.T, handler http.Handler, workspaceHandler http.HandlerFunc) *datasource.ReadResponse {
	t.Helper()
	ctx := context.Background()

	mux := http.NewServeMux()
	mux.Handle("/", handler)
	if workspaceHandler != nil {
		mux.Handle("/api/v1/workspace/ws-destination", workspaceHandler)
	} else {
		mux.HandleFunc("/api/v1/workspace/ws-destination", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
			fmt.Fprint(w, `{"data":{"type":"workspace","id":"ws-destination","attributes":{"name":"destination"},
				"relationships":{"organization":{"data":{"type":"organization","id":"org-1"}}}}}`)
		})
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	d := &WorkspaceRunTriggerDataSource{client: newTestClient(server.Client(), server.URL)}

	var schemaResp datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", schemaResp.Diagnostics)
	}
	objType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("expected schema type to be a tftypes.Object")
	}

	config := buildObjectValue(objType, map[string]tftypes.Value{
		"source_workspace_id":      tftypes.NewValue(tftypes.String, "ws-source"),
		"destination_workspace_id": tftypes.NewValue(tftypes.String, "ws-destination"),
	})
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: config}}, resp)
	return resp
}

func runTriggerJSON(id, source, destination, template string, enabled bool) string {
	templateData := "null"
	if template != "" {
		templateData = fmt.Sprintf(`{"type":"template","id":%q}`, template)
	}
	return fmt.Sprintf(`{"type":"runTrigger","id":%q,"attributes":{"enabled":%t},"relationships":{
		"sourceWorkspace":{"data":{"type":"workspace","id":%q}},
		"destinationWorkspace":{"data":{"type":"workspace","id":%q}},
		"template":{"data":%s}}}`, id, enabled, source, destination, templateData)
}

func TestWorkspaceRunTriggerDataSource_Read(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != runTriggerDestinationPath {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		want := `sourceWorkspace.id=="ws-source";destinationWorkspace.id=="ws-destination"`
		if got := r.URL.Query().Get("filter[runTrigger]"); got != want {
			t.Errorf("filter = %q, want %q", got, want)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		fmt.Fprintf(w, `{"data":[%s]}`, runTriggerJSON("rt-1", "ws-source", "ws-destination", "tpl-1", false))
	})

	resp := readWorkspaceRunTriggerDataSource(t, handler, nil)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	var state WorkspaceRunTriggerDataSourceModel
	if diags := resp.State.Get(context.Background(), &state); diags.HasError() {
		t.Fatalf("reading state: %v", diags)
	}
	if state.ID.ValueString() != "rt-1" || state.Enabled.ValueBool() || state.TemplateID.ValueString() != "tpl-1" ||
		state.SourceWorkspaceID.ValueString() != "ws-source" || state.DestinationWorkspaceID.ValueString() != "ws-destination" {
		t.Errorf("unexpected state: %+v", state)
	}
}

func TestWorkspaceRunTriggerDataSource_ReadWithoutTemplate(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"data":[%s]}`, runTriggerJSON("rt-1", "ws-source", "ws-destination", "", true))
	})
	resp := readWorkspaceRunTriggerDataSource(t, handler, nil)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	var state WorkspaceRunTriggerDataSourceModel
	if diags := resp.State.Get(context.Background(), &state); diags.HasError() {
		t.Fatalf("reading state: %v", diags)
	}
	if !state.TemplateID.IsNull() || !state.Enabled.ValueBool() {
		t.Errorf("unexpected state: %+v", state)
	}
}

func TestWorkspaceRunTriggerDataSource_ReadErrors(t *testing.T) {
	match := runTriggerJSON("rt-1", "ws-source", "ws-destination", "", true)
	tests := []struct {
		name    string
		status  int
		body    string
		summary string
	}{
		{"not found", http.StatusOK, `{"data":[]}`, "Workspace run trigger not found"},
		{"other source", http.StatusOK, `{"data":[` + runTriggerJSON("rt-2", "ws-other", "ws-destination", "", true) + `]}`, "Workspace run trigger not found"},
		{"other destination", http.StatusOK, `{"data":[` + runTriggerJSON("rt-2", "ws-source", "ws-other", "", true) + `]}`, "Workspace run trigger not found"},
		{"duplicate", http.StatusOK, `{"data":[` + match + `,` + runTriggerJSON("rt-2", "ws-source", "ws-destination", "", true) + `]}`, "Multiple workspace run triggers found"},
		{"api error", http.StatusForbidden, `{"errors":[{"detail":"forbidden"}]}`, "Error reading workspace run trigger: 403 Forbidden"},
		{"invalid payload", http.StatusOK, `not json`, "Error reading workspace run trigger"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			})
			resp := readWorkspaceRunTriggerDataSource(t, handler, nil)
			if !resp.Diagnostics.HasError() {
				t.Fatal("expected an error diagnostic")
			}
			if got := resp.Diagnostics.Errors()[0].Summary(); got != test.summary {
				t.Errorf("diagnostic = %q, want %q", got, test.summary)
			}
		})
	}
}

// A destination workspace that does not exist (or is not visible) has no run triggers: the same
// diagnostic as an empty list, not an API error.
func TestWorkspaceRunTriggerDataSource_DestinationWorkspaceNotFound(t *testing.T) {
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
	})
	resp := readWorkspaceRunTriggerDataSource(t, handler, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"errors":[{"detail":"not found"}]}`)
	})
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error diagnostic")
	}
	if got := resp.Diagnostics.Errors()[0].Summary(); got != "Workspace run trigger not found" {
		t.Errorf("diagnostic = %q, want %q", got, "Workspace run trigger not found")
	}
}
