package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
)

func TestVcsDataSource(t *testing.T) {
	runLookupDataSourceTests(t, lookupDataSourceCase{
		apiPath:    "/api/v1/organization/org-1/vcs",
		filterKey:  "filter[vcs]",
		entityType: "vcs",
		config:     map[string]string{"organization_id": "org-1"},
		matchAttrs: `"description":"Github","clientId":"cid","endpoint":"https://github.com","apiUrl":"https://api.github.com","status":"COMPLETED"`,
		wantState:  map[string]string{"id": "obj-1", "description": "Github", "client_id": "cid", "api_url": "https://api.github.com", "status": "COMPLETED"},
		notFound:   "Vcs connection not found",
		multiple:   "Multiple vcs connection entries found",
		apiError:   "Error reading vcs connection: 403 Forbidden",
		badPayload: "Unable to unmarshal payload",
		newSource: func(c *http.Client, endpoint string) datasource.DataSource {
			return &VcsDataSource{client: c, endpoint: endpoint, token: "test-token"}
		},
	})
}
