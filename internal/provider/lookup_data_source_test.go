package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// lookupDataSourceCase describes a data source that finds one object by name.
type lookupDataSourceCase struct {
	apiPath    string
	filterKey  string
	entityType string
	config     map[string]string // config attributes besides name
	matchAttrs string            // extra JSON attributes for the matching object
	wantState  map[string]string
	notFound   string
	multiple   string
	apiError   string
	badPayload string
	newSource  func(client *http.Client, endpoint string) datasource.DataSource
}

const lookupTestName = `my "name" & more`

func (tc lookupDataSourceCase) read(t *testing.T, handler http.Handler) *datasource.ReadResponse {
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
	values := map[string]tftypes.Value{"name": tftypes.NewValue(tftypes.String, lookupTestName)}
	for k, v := range tc.config {
		values[k] = tftypes.NewValue(tftypes.String, v)
	}
	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: buildObjectValue(objType, values)}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
	d.Read(ctx, req, resp)
	return resp
}

func (tc lookupDataSourceCase) object(id, name, attrs string) string {
	if attrs != "" {
		attrs = "," + attrs
	}
	return fmt.Sprintf(`{"type":%q,"id":%q,"attributes":{"name":%q%s}}`, tc.entityType, id, name, attrs)
}

func runLookupDataSourceTests(t *testing.T, tc lookupDataSourceCase) {
	t.Helper()

	t.Run("read", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != tc.apiPath {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
			if got := r.URL.Query().Get(tc.filterKey); got != `name=="my \"name\" & more"` {
				t.Errorf("filter = %q", got)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Errorf("Authorization = %q", got)
			}
			fmt.Fprint(w, `{"data":[`+tc.object("other", "other", tc.matchAttrs)+`,`+tc.object("obj-1", lookupTestName, tc.matchAttrs)+`]}`)
		})
		resp := tc.read(t, handler)
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

	errorTests := []struct {
		name    string
		status  int
		body    string
		summary string
	}{
		{"not found", http.StatusOK, `{"data":[]}`, tc.notFound},
		{"other name only", http.StatusOK, `{"data":[` + tc.object("1", "other", "") + `]}`, tc.notFound},
		{"wildcard is not a match", http.StatusOK, `{"data":[` + tc.object("1", "my *", "") + `]}`, tc.notFound},
		{"duplicate", http.StatusOK, `{"data":[` + tc.object("1", lookupTestName, "") + `,` + tc.object("2", lookupTestName, "") + `]}`, tc.multiple},
		{"api error", http.StatusForbidden, `{"errors":[{"detail":"forbidden"}]}`, tc.apiError},
		{"invalid payload", http.StatusOK, `not json`, tc.badPayload},
	}
	for _, test := range errorTests {
		t.Run(test.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			})
			resp := tc.read(t, handler)
			if !resp.Diagnostics.HasError() {
				t.Fatal("expected an error diagnostic")
			}
			if got := resp.Diagnostics.Errors()[0].Summary(); got != test.summary {
				t.Errorf("diagnostic = %q, want %q", got, test.summary)
			}
		})
	}
}

type lookupTestEntity struct{ Name string }

func TestFindSingleByName(t *testing.T) {
	nameOf := func(e *lookupTestEntity) string { return e.Name }
	a, b := &lookupTestEntity{"a"}, &lookupTestEntity{"b"}

	var diags diag.Diagnostics
	if got := findSingleByName(&diags, "thing", "a", "", []interface{}{b, a}, nameOf); got != a || diags.HasError() {
		t.Errorf("expected a without error, got %v, %v", got, diags)
	}

	diags = nil
	if got := findSingleByName(&diags, "thing", "z", " in organization o", []interface{}{a, b}, nameOf); got != nil ||
		len(diags.Errors()) != 1 || diags.Errors()[0].Summary() != "Thing not found" ||
		!strings.Contains(diags.Errors()[0].Detail(), `No thing named "z" was found in organization o.`) {
		t.Errorf("unexpected result for no match: %v, %v", got, diags)
	}

	diags = nil
	if got := findSingleByName(&diags, "thing", "a", "", []interface{}{a, &lookupTestEntity{"a"}}, nameOf); got != nil ||
		len(diags.Errors()) != 1 || diags.Errors()[0].Summary() != "Multiple thing entries found" {
		t.Errorf("unexpected result for duplicates: %v, %v", got, diags)
	}

	diags = nil
	if got := findSingleByName(&diags, "thing", "a", "", []interface{}{"not an entity"}, nameOf); got != nil || !diags.HasError() {
		t.Errorf("expected an error for an unexpected type, got %v, %v", got, diags)
	}
}
