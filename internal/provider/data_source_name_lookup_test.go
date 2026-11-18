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

// The workspace, policy_set and policy_exemption data sources look objects up by name with an RSQL
// filter. These tests check the same rules #257 added to the other data sources: the name is
// quoted, and the result must be exactly one object with that exact name, since RSQL treats `*`
// in a value as a wildcard.

// nameLookupCase describes one data source: how to build it, its config, and the path and
// response of the lookup below the organization.
type nameLookupCase struct {
	name       string
	newDS      func(serverURL string, c *http.Client) datasource.DataSource
	config     map[string]string
	childPath  string
	childParam string
	childValue string
	child      func(names ...string) string
}

func nameLookupCases() []nameLookupCase {
	return []nameLookupCase{
		{
			name: "workspace",
			newDS: func(u string, c *http.Client) datasource.DataSource {
				return &WorkspaceDataSource{client: c, endpoint: u, token: "test-token"}
			},
			config:     map[string]string{"organization": "my org", "name": "prod & more"},
			childPath:  "/api/v1/organization/org-1/workspace",
			childParam: "filter[workspace]",
			childValue: "prod & more",
			child:      jsonapiNamed("workspace"),
		},
		{
			name: "policy_set",
			newDS: func(u string, c *http.Client) datasource.DataSource {
				return &PolicySetDataSource{client: c, endpoint: u, token: "test-token"}
			},
			config:     map[string]string{"organization": "my org", "name": "prod & more"},
			childPath:  "/api/v1/organization/org-1/policySet",
			childParam: "filter[policy_set]",
			childValue: "prod & more",
			child:      jsonapiNamed("policy_set"),
		},
		{
			name: "policy_exemption",
			newDS: func(u string, c *http.Client) datasource.DataSource {
				return &PolicyExemptionDataSource{client: c, endpoint: u, token: "test-token"}
			},
			config:     map[string]string{"organization": "my org", "rule_id": "rule;one"},
			childPath:  "/api/v1/organization/org-1/policyExemption",
			childParam: "filter[policy_exemption]",
			childValue: "rule;one",
			child: func(...string) string {
				return `{"data":[{"type":"policy_exemption","id":"ex-1","attributes":{"ruleId":"rule;one"}}]}`
			},
		},
	}
}

// jsonapiNamed returns a JSON:API list of objects of the given type, one per name.
func jsonapiNamed(kind string) func(names ...string) string {
	return func(names ...string) string {
		body := `{"data":[`
		for i, n := range names {
			if i > 0 {
				body += ","
			}
			body += fmt.Sprintf(`{"type":%q,"id":"id-%d","attributes":{"name":%q}}`, kind, i, n)
		}
		return body + `]}`
	}
}

type nameLookupServer struct {
	orgs    string
	child   string
	filters map[string]string
}

func (c nameLookupCase) read(t *testing.T, s *nameLookupServer) *datasource.ReadResponse {
	t.Helper()
	ctx := context.Background()
	s.filters = map[string]string{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization", func(w http.ResponseWriter, r *http.Request) {
		s.filters["filter[organization]"] = r.URL.Query().Get("filter[organization]")
		fmt.Fprint(w, s.orgs)
	})
	mux.HandleFunc(c.childPath, func(w http.ResponseWriter, r *http.Request) {
		s.filters[c.childParam] = r.URL.Query().Get(c.childParam)
		fmt.Fprint(w, s.child)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	d := c.newDS(server.URL, server.Client())
	var schemaResp datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
	objType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("expected schema type to be a tftypes.Object")
	}
	values := map[string]tftypes.Value{}
	for k, v := range c.config {
		values[k] = tftypes.NewValue(tftypes.String, v)
	}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: buildObjectValue(objType, values)}}, resp)
	return resp
}

const lookupOrg = `{"data":[{"type":"organization","id":"org-1","attributes":{"name":"my org"}}]}`

func TestDataSourceNameLookup_QuotesNames(t *testing.T) {
	for _, c := range nameLookupCases() {
		t.Run(c.name, func(t *testing.T) {
			s := &nameLookupServer{orgs: lookupOrg, child: c.child(c.childValue)}
			resp := c.read(t, s)
			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
			}
			if got, want := s.filters["filter[organization]"], `name=="my org"`; got != want {
				t.Errorf("organization filter = %q, want %q", got, want)
			}
			if got := s.filters[c.childParam]; got != `name=="`+c.childValue+`"` && got != `ruleId=="`+c.childValue+`"` {
				t.Errorf("%s = %q, want the value quoted", c.childParam, got)
			}
		})
	}
}

func TestDataSourceNameLookup_OrganizationMustMatchExactly(t *testing.T) {
	for _, c := range nameLookupCases() {
		t.Run(c.name, func(t *testing.T) {
			// What a wildcard filter would return: another organization only.
			s := &nameLookupServer{
				orgs:  `{"data":[{"type":"organization","id":"org-1","attributes":{"name":"my org 2"}}]}`,
				child: c.child(c.childValue),
			}
			errs := c.read(t, s).Diagnostics.Errors()
			if len(errs) != 1 || errs[0].Summary() != "Organization not found" {
				t.Fatalf("expected one \"Organization not found\" error, got %v", errs)
			}
		})
	}
}

func TestDataSourceNameLookup_ChildMustBeUnique(t *testing.T) {
	for _, c := range nameLookupCases()[:2] { // exemptions are looked up by rule ID, not by name
		t.Run(c.name, func(t *testing.T) {
			s := &nameLookupServer{orgs: lookupOrg, child: c.child(c.childValue, c.childValue)}
			errs := c.read(t, s).Diagnostics.Errors()
			want := fmt.Sprintf("Multiple %s entries found", map[string]string{"workspace": "workspace", "policy_set": "policy set"}[c.name])
			if len(errs) != 1 || errs[0].Summary() != want {
				t.Fatalf("expected one %q error, got %v", want, errs)
			}
		})
	}
}

func TestDataSourceNameLookup_ChildMustMatchExactly(t *testing.T) {
	for _, c := range nameLookupCases()[:2] {
		t.Run(c.name, func(t *testing.T) {
			s := &nameLookupServer{orgs: lookupOrg, child: c.child(c.childValue + " 2")}
			errs := c.read(t, s).Diagnostics.Errors()
			want := map[string]string{"workspace": "Workspace not found", "policy_set": "Policy set not found"}[c.name]
			if len(errs) != 1 || errs[0].Summary() != want {
				t.Fatalf("expected one %q error, got %v", want, errs)
			}
		})
	}
}
