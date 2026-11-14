package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// accessDataSourceCase describes the project and workspace access data sources, which only
// differ in the parent they are scoped to.
type accessDataSourceCase struct {
	parent     string // "project" or "workspace"
	apiPath    string
	filterKey  string
	entityType string
	notFound   string
	multiple   string
	apiError   string
	badPayload string
	newSource  func(client *http.Client, endpoint string) datasource.DataSource
}

func (tc accessDataSourceCase) read(t *testing.T, name string, handler http.Handler) *datasource.ReadResponse {
	t.Helper()
	ctx := context.Background()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	d := tc.newSource(server.Client(), server.URL)

	var schemaResp datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics building schema: %v", schemaResp.Diagnostics)
	}
	objType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("expected schema type to be a tftypes.Object")
	}
	values := map[string]tftypes.Value{
		"organization_id": tftypes.NewValue(tftypes.String, "org-1"),
		tc.parent + "_id": tftypes.NewValue(tftypes.String, tc.parent+"-1"),
		"name":            tftypes.NewValue(tftypes.String, name),
	}
	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: buildObjectValue(objType, values)}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
	d.Read(ctx, req, resp)
	return resp
}

func (tc accessDataSourceCase) item(id, name, extra string) string {
	return fmt.Sprintf(`{"type":%q,"id":%q,"attributes":{"name":%q,"manageState":true,"manageJob":false,"manageWorkspace":true,"planJob":true,"approveJob":false%s}}`,
		tc.entityType, id, name, extra)
}

func runAccessDataSourceTests(t *testing.T, tc accessDataSourceCase) {
	t.Run("read", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != tc.apiPath {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
			if got := r.URL.Query().Get(tc.filterKey); got != `name=="platform team"` {
				t.Errorf("filter = %q", got)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Errorf("Authorization = %q", got)
			}
			fmt.Fprint(w, `{"data":[`+tc.item("other", "other team", "")+`,`+
				tc.item("acc-1", "platform team", `,"destroyJob":false,"role":"write"`)+`]}`)
		})
		resp := tc.read(t, "platform team", handler)
		if resp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
		}
		ctx := context.Background()
		var id, role string
		var manageState, manageJob, manageWorkspace, planJob, approveJob, destroyJob bool
		resp.State.GetAttribute(ctx, path.Root("id"), &id)
		resp.State.GetAttribute(ctx, path.Root("role"), &role)
		resp.State.GetAttribute(ctx, path.Root("manage_state"), &manageState)
		resp.State.GetAttribute(ctx, path.Root("manage_job"), &manageJob)
		resp.State.GetAttribute(ctx, path.Root("manage_workspace"), &manageWorkspace)
		resp.State.GetAttribute(ctx, path.Root("plan_job"), &planJob)
		resp.State.GetAttribute(ctx, path.Root("approve_job"), &approveJob)
		resp.State.GetAttribute(ctx, path.Root("destroy_job"), &destroyJob)
		if id != "acc-1" || role != "write" || !manageState || manageJob || !manageWorkspace || !planJob || approveJob || destroyJob {
			t.Errorf("unexpected state: id=%s role=%s manage_state=%v manage_job=%v manage_workspace=%v plan_job=%v approve_job=%v destroy_job=%v",
				id, role, manageState, manageJob, manageWorkspace, planJob, approveJob, destroyJob)
		}
	})

	t.Run("role and destroy_job are null when the API omits them", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"data":[`+tc.item("acc-1", "platform team", "")+`]}`)
		})
		resp := tc.read(t, "platform team", handler)
		if resp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
		}
		var role types.String
		var destroyJob types.Bool
		resp.State.GetAttribute(context.Background(), path.Root("role"), &role)
		resp.State.GetAttribute(context.Background(), path.Root("destroy_job"), &destroyJob)
		if !role.IsNull() || !destroyJob.IsNull() {
			t.Errorf("role=%v destroy_job=%v, want both null", role, destroyJob)
		}
	})

	errorTests := []struct {
		name    string
		status  int
		body    string
		summary string
	}{
		{"not found", http.StatusOK, `{"data":[]}`, tc.notFound},
		{"other team", http.StatusOK, `{"data":[` + tc.item("1", "other team", "") + `]}`, tc.notFound},
		{"duplicate", http.StatusOK, `{"data":[` + tc.item("1", "platform team", "") + `,` + tc.item("2", "platform team", "") + `]}`, tc.multiple},
		{"api error", http.StatusForbidden, `{"errors":[{"detail":"forbidden"}]}`, tc.apiError},
		{"invalid payload", http.StatusOK, `not json`, tc.badPayload},
	}
	for _, test := range errorTests {
		t.Run(test.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			})
			resp := tc.read(t, "platform team", handler)
			if !resp.Diagnostics.HasError() {
				t.Fatal("expected an error diagnostic")
			}
			if got := resp.Diagnostics.Errors()[0].Summary(); got != test.summary {
				t.Errorf("diagnostic = %q, want %q", got, test.summary)
			}
		})
	}
}
