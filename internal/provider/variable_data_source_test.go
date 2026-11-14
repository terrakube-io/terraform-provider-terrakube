package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// variableDataSourceCase describes one of the two variable data sources.
type variableDataSourceCase struct {
	name       string
	apiPath    string
	filterKey  string
	entityType string
	extraAttrs map[string]string
	newSource  func(client *http.Client, endpoint string) datasource.DataSource
}

var variableDataSourceCases = []variableDataSourceCase{
	{
		name:       "organization",
		apiPath:    "/api/v1/organization/org-1/globalvar",
		filterKey:  "filter[globalvar]",
		entityType: "globalvar",
		newSource: func(c *http.Client, endpoint string) datasource.DataSource {
			return &OrganizationVariableDataSource{client: c, endpoint: endpoint, token: "test-token"}
		},
	},
	{
		name:       "workspace",
		apiPath:    "/api/v1/organization/org-1/workspace/ws-1/variable",
		filterKey:  "filter[variable]",
		entityType: "variable",
		extraAttrs: map[string]string{"workspace_id": "ws-1"},
		newSource: func(c *http.Client, endpoint string) datasource.DataSource {
			return &WorkspaceVariableDataSource{client: c, endpoint: endpoint, token: "test-token"}
		},
	},
}

func (tc variableDataSourceCase) read(t *testing.T, key, category string, handler http.Handler) *datasource.ReadResponse {
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
		"key":             tftypes.NewValue(tftypes.String, key),
	}
	if category != "" {
		values["category"] = tftypes.NewValue(tftypes.String, category)
	}
	for name, value := range tc.extraAttrs {
		values[name] = tftypes.NewValue(tftypes.String, value)
	}
	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: buildObjectValue(objType, values)}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
	d.Read(ctx, req, resp)
	return resp
}

func (tc variableDataSourceCase) body(items ...string) string {
	out := `{"data":[`
	for i, item := range items {
		if i > 0 {
			out += ","
		}
		out += item
	}
	return out + `]}`
}

func (tc variableDataSourceCase) item(id, key, category, value string, extra string) string {
	return fmt.Sprintf(`{"type":%q,"id":%q,"attributes":{"key":%q,"category":%q,"value":%q,"description":"desc","hcl":false%s}}`,
		tc.entityType, id, key, category, value, extra)
}

func TestVariableDataSources_Read(t *testing.T) {
	for _, tc := range variableDataSourceCases {
		t.Run(tc.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != tc.apiPath {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				if got := r.URL.Query().Get(tc.filterKey); got != `key=="my key";category=="ENV"` {
					t.Errorf("filter = %q", got)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
					t.Errorf("Authorization = %q", got)
				}
				fmt.Fprint(w, tc.body(
					tc.item("other", "other key", "ENV", "no", `,"sensitive":false`),
					tc.item("var-1", "my key", "ENV", "hello", `,"sensitive":false`),
				))
			})

			resp := tc.read(t, "my key", "ENV", handler)
			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
			}
			get := func(attr string) string {
				var v string
				resp.State.GetAttribute(context.Background(), path.Root(attr), &v)
				return v
			}
			if get("id") != "var-1" || get("value") != "hello" || get("category") != "ENV" || get("description") != "desc" {
				t.Errorf("unexpected state: id=%s value=%s category=%s", get("id"), get("value"), get("category"))
			}
		})
	}
}

func TestVariableDataSources_SensitiveValueIsNull(t *testing.T) {
	for _, tc := range variableDataSourceCases {
		t.Run(tc.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, tc.body(tc.item("var-1", "token", "ENV", "should-not-be-read", `,"sensitive":true`)))
			})
			resp := tc.read(t, "token", "", handler)
			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
			}
			var value types.String
			var sensitive types.Bool
			resp.State.GetAttribute(context.Background(), path.Root("value"), &value)
			resp.State.GetAttribute(context.Background(), path.Root("sensitive"), &sensitive)
			if !sensitive.ValueBool() || !value.IsNull() {
				t.Errorf("sensitive=%v value=%v, want true and null", sensitive, value)
			}
		})
	}
}

// The organization variable API can omit "sensitive". It is read as false.
func TestVariableDataSources_MissingSensitiveIsFalse(t *testing.T) {
	for _, tc := range variableDataSourceCases {
		t.Run(tc.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, tc.body(tc.item("var-1", "plain", "TERRAFORM", "x", "")))
			})
			resp := tc.read(t, "plain", "", handler)
			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
			}
			var value string
			var sensitive bool
			resp.State.GetAttribute(context.Background(), path.Root("value"), &value)
			resp.State.GetAttribute(context.Background(), path.Root("sensitive"), &sensitive)
			if sensitive || value != "x" {
				t.Errorf("sensitive=%v value=%q, want false and x", sensitive, value)
			}
		})
	}
}

func TestVariableDataSources_Category(t *testing.T) {
	for _, tc := range variableDataSourceCases {
		t.Run(tc.name, func(t *testing.T) {
			both := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, tc.body(
					tc.item("env-1", "region", "ENV", "a", `,"sensitive":false`),
					tc.item("tf-1", "region", "TERRAFORM", "b", `,"sensitive":false`),
				))
			})

			resp := tc.read(t, "region", "", both)
			if !resp.Diagnostics.HasError() || resp.Diagnostics.Errors()[0].Summary() != fmt.Sprintf("Multiple %s variables found", tc.name) {
				t.Errorf("expected the ambiguity error, got %v", resp.Diagnostics)
			}

			resp = tc.read(t, "region", "TERRAFORM", both)
			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
			}
			var id string
			resp.State.GetAttribute(context.Background(), path.Root("id"), &id)
			if id != "tf-1" {
				t.Errorf("id = %q, want tf-1", id)
			}
		})
	}
}

func TestVariableDataSources_ReadErrors(t *testing.T) {
	for _, tc := range variableDataSourceCases {
		tests := []struct {
			name    string
			status  int
			body    string
			summary string
		}{
			{"not found", http.StatusOK, `{"data":[]}`, fmt.Sprintf("%s variable not found", capitalize(tc.name))},
			{"other key", http.StatusOK, tc.body(tc.item("1", "other", "ENV", "", `,"sensitive":false`)), fmt.Sprintf("%s variable not found", capitalize(tc.name))},
			{"api error", http.StatusForbidden, `{"errors":[{"detail":"forbidden"}]}`, fmt.Sprintf("Error reading %s variable: 403 Forbidden", tc.name)},
			{"invalid payload", http.StatusOK, `not json`, fmt.Sprintf("Unable to unmarshal %s variable payload", tc.name)},
		}
		for _, test := range tests {
			t.Run(tc.name+"/"+test.name, func(t *testing.T) {
				handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(test.status)
					fmt.Fprint(w, test.body)
				})
				resp := tc.read(t, "wanted", "", handler)
				if !resp.Diagnostics.HasError() {
					t.Fatal("expected an error diagnostic")
				}
				if got := resp.Diagnostics.Errors()[0].Summary(); got != test.summary {
					t.Errorf("diagnostic = %q, want %q", got, test.summary)
				}
			})
		}
	}
}

func capitalize(s string) string {
	return strings.ToUpper(s[:1]) + s[1:]
}
