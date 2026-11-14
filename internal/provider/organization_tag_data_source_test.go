package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
)

func TestOrganizationTagDataSource(t *testing.T) {
	runLookupDataSourceTests(t, lookupDataSourceCase{
		apiPath:    "/api/v1/organization/org-1/tag",
		filterKey:  "filter[tag]",
		entityType: "tag",
		config:     map[string]string{"organization_id": "org-1"},
		matchAttrs: ``,
		wantState:  map[string]string{"id": "obj-1"},
		notFound:   "Organization tag not found",
		multiple:   "Multiple organization tag entries found",
		apiError:   "Error reading organization tag: 403 Forbidden",
		badPayload: "Unable to unmarshal payload",
		newSource: func(c *http.Client, endpoint string) datasource.DataSource {
			return &OrganizationTagDataSource{client: c, endpoint: endpoint, token: "test-token"}
		},
	})
}
