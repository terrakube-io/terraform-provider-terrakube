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

func readWorkspaceScheduleDataSource(t *testing.T, cron *string, handler http.Handler) *datasource.ReadResponse {
	t.Helper()
	ctx := context.Background()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	d := &WorkspaceScheduleDataSource{client: newTestClient(server.Client(), server.URL)}

	var schemaResp datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", schemaResp.Diagnostics)
	}
	objType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("expected schema type to be a tftypes.Object")
	}

	overrides := map[string]tftypes.Value{
		"workspace_id": tftypes.NewValue(tftypes.String, "ws-1"),
		"template_id":  tftypes.NewValue(tftypes.String, "tpl-apply"),
	}
	if cron != nil {
		overrides["schedule"] = tftypes.NewValue(tftypes.String, *cron)
	}
	config := buildObjectValue(objType, overrides)
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: config}}, resp)
	return resp
}

func scheduleJSON(id, cron, template string) string {
	return fmt.Sprintf(`{"type":"schedule","id":%q,"attributes":{"cron":%q,"templateReference":%q}}`, id, cron, template)
}

func readScheduleState(t *testing.T, resp *datasource.ReadResponse) WorkspaceScheduleDataSourceModel {
	t.Helper()
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	var state WorkspaceScheduleDataSourceModel
	if diags := resp.State.Get(context.Background(), &state); diags.HasError() {
		t.Fatalf("reading state: %v", diags)
	}
	return state
}

func TestWorkspaceScheduleDataSource_ReadByTemplate(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/workspace/ws-1/schedule" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got, want := r.URL.Query().Get("filter[schedule]"), `templateReference=="tpl-apply"`; got != want {
			t.Errorf("filter = %q, want %q", got, want)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		fmt.Fprintf(w, `{"data":[%s]}`, scheduleJSON("sch-1", "0 0 6 ? * MON-FRI", "tpl-apply"))
	})

	state := readScheduleState(t, readWorkspaceScheduleDataSource(t, nil, handler))
	if state.ID.ValueString() != "sch-1" || state.Schedule.ValueString() != "0 0 6 ? * MON-FRI" ||
		state.WorkspaceId.ValueString() != "ws-1" || state.TemplateId.ValueString() != "tpl-apply" {
		t.Errorf("unexpected state: %+v", state)
	}
}

func TestWorkspaceScheduleDataSource_ReadByTemplateAndSchedule(t *testing.T) {
	cron := "0 0 8 ? * SAT,SUN"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := `templateReference=="tpl-apply";cron=="0 0 8 ? * SAT,SUN"`
		if got := r.URL.Query().Get("filter[schedule]"); got != want {
			t.Errorf("filter = %q, want %q", got, want)
		}
		// Return both schedules for the template, as a server that ignores the
		// cron filter would, to check the provider picks the matching one.
		fmt.Fprintf(w, `{"data":[%s,%s]}`,
			scheduleJSON("sch-1", "0 0 6 ? * MON-FRI", "tpl-apply"),
			scheduleJSON("sch-2", cron, "tpl-apply"))
	})

	state := readScheduleState(t, readWorkspaceScheduleDataSource(t, &cron, handler))
	if state.ID.ValueString() != "sch-2" || state.Schedule.ValueString() != cron {
		t.Errorf("unexpected state: %+v", state)
	}
}

func TestWorkspaceScheduleDataSource_ReadErrors(t *testing.T) {
	cron := "0 0 8 ? * SAT,SUN"
	tests := []struct {
		name    string
		cron    *string
		status  int
		body    string
		summary string
	}{
		{"not found", nil, http.StatusOK, `{"data":[]}`, "Workspace schedule not found"},
		{"other template", nil, http.StatusOK, `{"data":[` + scheduleJSON("sch-1", "0 0 2 * * ?", "tpl-plan") + `]}`, "Workspace schedule not found"},
		{"other schedule", &cron, http.StatusOK, `{"data":[` + scheduleJSON("sch-1", "0 0 6 ? * MON-FRI", "tpl-apply") + `]}`, "Workspace schedule not found"},
		{"ambiguous", nil, http.StatusOK, `{"data":[` + scheduleJSON("sch-1", "0 0 6 ? * MON-FRI", "tpl-apply") + `,` + scheduleJSON("sch-2", cron, "tpl-apply") + `]}`, "Multiple workspace schedules found"},
		{"api error", nil, http.StatusForbidden, `{"errors":[{"detail":"forbidden"}]}`, "Error reading workspace schedule: 403 Forbidden"},
		{"invalid payload", nil, http.StatusOK, `not json`, "Error reading workspace schedule"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			})
			resp := readWorkspaceScheduleDataSource(t, test.cron, handler)
			if !resp.Diagnostics.HasError() {
				t.Fatal("expected an error diagnostic")
			}
			if got := resp.Diagnostics.Errors()[0].Summary(); got != test.summary {
				t.Errorf("diagnostic = %q, want %q", got, test.summary)
			}
		})
	}
}
