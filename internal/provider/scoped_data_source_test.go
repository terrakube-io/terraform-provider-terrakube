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
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// scopedDataSourceCase describes a data source that resolves an organization by name first
// and then finds one object by name inside it.
type scopedDataSourceCase struct {
	entityPath string
	filterKey  string
	entityType string
	matchAttrs string
	wantState  map[string]string
	nameAttr   string // attribute holding the object name in the configuration
	notFound   string
	multiple   string
	apiError   string
	newSource  func(client *http.Client, endpoint string) datasource.DataSource
}

const scopedOrgJSON = `{"data":[{"type":"organization","id":"org-1","attributes":{"name":"my-org"}}]}`

type scopedResponse struct {
	status int
	body   string
}

func (tc scopedDataSourceCase) read(t *testing.T, org, entity scopedResponse, check func(r *http.Request)) *datasource.ReadResponse {
	t.Helper()
	ctx := context.Background()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			check(r)
		}
		out := entity
		if r.URL.Path == "/api/v1/organization" {
			out = org
		}
		w.WriteHeader(out.status)
		fmt.Fprint(w, out.body)
	})
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
		"organization": tftypes.NewValue(tftypes.String, "my-org"),
		tc.nameAttr:    tftypes.NewValue(tftypes.String, lookupTestName),
	}
	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: buildObjectValue(objType, values)}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
	d.Read(ctx, req, resp)
	return resp
}

func (tc scopedDataSourceCase) object(id, name, attrs string) string {
	if attrs != "" {
		attrs = "," + attrs
	}
	return fmt.Sprintf(`{"type":%q,"id":%q,"attributes":{"name":%q%s}}`, tc.entityType, id, name, attrs)
}

func runScopedDataSourceTests(t *testing.T, tc scopedDataSourceCase) {
	t.Helper()
	ok := func(body string) scopedResponse { return scopedResponse{http.StatusOK, body} }
	org := ok(scopedOrgJSON)

	t.Run("read", func(t *testing.T) {
		resp := tc.read(t, org, ok(`{"data":[`+tc.object("other", "other", tc.matchAttrs)+`,`+tc.object("obj-1", lookupTestName, tc.matchAttrs)+`]}`),
			func(r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/organization":
					if got := r.URL.Query().Get("filter[organization]"); got != `name=="my-org"` {
						t.Errorf("organization filter = %q", got)
					}
				case tc.entityPath:
					if got := r.URL.Query().Get(tc.filterKey); got != `name=="my \"name\" & more"` {
						t.Errorf("filter = %q", got)
					}
				default:
					t.Errorf("unexpected request: %s", r.URL)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
					t.Errorf("Authorization = %q", got)
				}
			})
		if resp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
		}
		for attr, want := range tc.wantState {
			var got string
			resp.State.GetAttribute(context.Background(), path.Root(attr), &got)
			if got != want {
				t.Errorf("%s = %q, want %q", attr, got, want)
			}
		}
	})

	forbidden := scopedResponse{http.StatusForbidden, `{"errors":[{"detail":"forbidden"}]}`}
	tests := []struct {
		name    string
		org     scopedResponse
		entity  scopedResponse
		summary string
	}{
		{"organization not found", ok(`{"data":[]}`), ok(`{"data":[]}`), "Organization not found"},
		{"organization api error", forbidden, ok(`{"data":[]}`), tc.apiError},
		{"not found", org, ok(`{"data":[]}`), tc.notFound},
		{"other name only", org, ok(`{"data":[` + tc.object("1", "other", "") + `]}`), tc.notFound},
		{"wildcard is not a match", org, ok(`{"data":[` + tc.object("1", "my *", "") + `]}`), tc.notFound},
		{"duplicate", org, ok(`{"data":[` + tc.object("1", lookupTestName, "") + `,` + tc.object("2", lookupTestName, "") + `]}`), tc.multiple},
		{"api error", org, forbidden, tc.apiError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp := tc.read(t, test.org, test.entity, nil)
			errs := resp.Diagnostics.Errors()
			if len(errs) != 1 {
				t.Fatalf("expected exactly one error, got %d: %v", len(errs), errs)
			}
			if got := errs[0].Summary(); got != test.summary {
				t.Errorf("diagnostic = %q, want %q", got, test.summary)
			}
		})
	}
}
