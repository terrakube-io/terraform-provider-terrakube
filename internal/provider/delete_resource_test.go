package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// A DELETE that Terrakube refuses must fail the delete. Several resources used to ignore the
// response, so a 403 or a 500 counted as success and the object left the state while it still
// existed. A 404 is fine: the object is already gone.
func TestResourceDelete_ReportsFailures(t *testing.T) {

	cases := []struct {
		name string
		path string
		make func(c *http.Client, endpoint string) resource.Resource
	}{
		{"collection", "/api/v1/organization/org-1/collection/item-1", func(c *http.Client, e string) resource.Resource {
			return &CollectionResource{client: newTestClient(c, e)}
		}},
		{"organization_variable", "/api/v1/organization/org-1/globalvar/item-1", func(c *http.Client, e string) resource.Resource {
			return &OrganizationVariableResource{client: c, endpoint: e, token: "test-token"}
		}},
		{"workspace_variable", "/api/v1/organization/org-1/workspace/ws-1/variable/item-1", func(c *http.Client, e string) resource.Resource {
			return &WorkspaceVariableResource{client: c, endpoint: e, token: "test-token"}
		}},
		{"self_hosted_agent", "/api/v1/organization/org-1/agent/item-1", func(c *http.Client, e string) resource.Resource {
			return &AgentResource{client: c, endpoint: e, token: "test-token"}
		}},
		{"ssh", "/api/v1/organization/org-1/ssh/item-1", func(c *http.Client, e string) resource.Resource {
			return &SshResource{client: c, endpoint: e, token: "test-token"}
		}},
		{"module", "/api/v1/organization/org-1/module/item-1", func(c *http.Client, e string) resource.Resource {
			return &ModuleResource{client: c, endpoint: e, token: "test-token"}
		}},
		{"organization_notification_configuration", "/api/v1/notification_configuration/item-1", func(c *http.Client, e string) resource.Resource {
			return &OrganizationNotificationConfigurationResource{client: c, endpoint: e, token: "test-token"}
		}},
		{"workspace_notification_configuration", "/api/v1/notification_configuration/item-1", func(c *http.Client, e string) resource.Resource {
			return &WorkspaceNotificationConfigurationResource{client: c, endpoint: e, token: "test-token"}
		}},
		{"collection_item", "/api/v1/organization/org-1/collection/coll-1/item/item-1", func(c *http.Client, e string) resource.Resource {
			return &CollectionItemResource{client: newTestClient(c, e)}
		}},
		{"collection_reference", "/api/v1/reference/item-1", func(c *http.Client, e string) resource.Resource {
			return &CollectionReferenceResource{client: newTestClient(c, e)}
		}},
		{"workspace_schedule", "/api/v1/workspace/ws-1/schedule/item-1", func(c *http.Client, e string) resource.Resource {
			return &WorkspaceScheduleResource{client: c, endpoint: e, token: "test-token"}
		}},
		{"federated_credential", "/api/v1/federated/item-1", func(c *http.Client, e string) resource.Resource {
			return &FederatedCredentialResource{client: c, endpoint: e, token: "test-token"}
		}},
		{"federated_credential_claim", "/api/v1/federated/fc-1/claims/item-1", func(c *http.Client, e string) resource.Resource {
			return &FederatedCredentialClaimResource{client: c, endpoint: e, token: "test-token"}
		}},
		// Already handled its own errors before; kept as the control for the others.
		{"workspace_run_trigger", "/api/v1/runTrigger/item-1", func(c *http.Client, e string) resource.Resource {
			return &WorkspaceRunTriggerResource{client: c, endpoint: e, token: "test-token"}
		}},
	}

	outcomes := []struct {
		status  int
		wantErr bool
	}{
		{http.StatusNoContent, false},
		{http.StatusOK, false},
		{http.StatusNotFound, false},
		{http.StatusForbidden, true},
		{http.StatusInternalServerError, true},
	}

	for _, tc := range cases {
		for _, outcome := range outcomes {
			t.Run(fmt.Sprintf("%s/%d", tc.name, outcome.status), func(t *testing.T) {
				ctx := context.Background()
				var gotMethod, gotPath, gotAuth string
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
					w.WriteHeader(outcome.status)
					if outcome.status >= 400 && outcome.status != http.StatusNotFound {
						fmt.Fprint(w, `{"errors":[{"detail":"the server said no"}]}`)
					}
				}))
				defer server.Close()

				res := tc.make(server.Client(), server.URL)
				var schemaResp resource.SchemaResponse
				res.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
				objType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
				if !ok {
					t.Fatal("expected schema type to be a tftypes.Object")
				}
				str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
				// Parent IDs the resource does not have are ignored by buildObjectValue.
				state := tfsdk.State{Schema: schemaResp.Schema, Raw: buildObjectValue(objType, map[string]tftypes.Value{
					"id":                      str("item-1"),
					"organization_id":         str("org-1"),
					"workspace_id":            str("ws-1"),
					"collection_id":           str("coll-1"),
					"federated_credential_id": str("fc-1"),
				})}

				resp := &resource.DeleteResponse{State: state}
				res.Delete(ctx, resource.DeleteRequest{State: state}, resp)

				if gotMethod != http.MethodDelete || gotPath != tc.path {
					t.Errorf("request = %s %s, want DELETE %s", gotMethod, gotPath, tc.path)
				}
				if gotAuth != "Bearer test-token" {
					t.Errorf("Authorization = %q", gotAuth)
				}
				switch {
				case outcome.wantErr && !resp.Diagnostics.HasError():
					t.Errorf("a %d must fail the delete", outcome.status)
				case !outcome.wantErr && resp.Diagnostics.HasError():
					t.Errorf("a %d must not fail the delete: %v", outcome.status, resp.Diagnostics)
				}
				if outcome.wantErr && resp.Diagnostics.HasError() {
					summary := resp.Diagnostics.Errors()[0].Summary()
					if !strings.Contains(summary, fmt.Sprint(outcome.status)) && !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), fmt.Sprint(outcome.status)) {
						t.Errorf("the error should name the status %d, got %q", outcome.status, summary)
					}
				}
			})
		}
	}
}
