package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
)

func TestSshDataSource(t *testing.T) {
	runLookupDataSourceTests(t, lookupDataSourceCase{
		apiPath:    "/api/v1/organization/org-1/ssh",
		filterKey:  "filter[ssh]",
		entityType: "ssh",
		config:     map[string]string{"organization_id": "org-1"},
		matchAttrs: `"description":"Deploy key"`,
		wantState:  map[string]string{"id": "obj-1", "description": "Deploy key"},
		notFound:   "Ssh key not found",
		multiple:   "Multiple ssh key entries found",
		apiError:   "Error reading ssh key: 403 Forbidden",
		badPayload: "Unable to unmarshal payload",
		newSource: func(c *http.Client, endpoint string) datasource.DataSource {
			return &SshDataSource{client: c, endpoint: endpoint, token: "test-token"}
		},
	})
}
