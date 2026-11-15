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

func readWorkspaceRunTriggerDataSource(t *testing.T, handler http.Handler) *datasource.ReadResponse {
	t.Helper()
	ctx := context.Background()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	d := &WorkspaceRunTriggerDataSource{client: server.Client(), endpoint: server.URL, token: "test-token"}

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
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/runTrigger" {
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

	resp := readWorkspaceRunTriggerDataSource(t, handler)
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
	resp := readWorkspaceRunTriggerDataSource(t, handler)
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
		{"invalid payload", http.StatusOK, `not json`, "Unable to unmarshal workspace run trigger payload"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			})
			resp := readWorkspaceRunTriggerDataSource(t, handler)
			if !resp.Diagnostics.HasError() {
				t.Fatal("expected an error diagnostic")
			}
			if got := resp.Diagnostics.Errors()[0].Summary(); got != test.summary {
				t.Errorf("diagnostic = %q, want %q", got, test.summary)
			}
		})
	}
}
