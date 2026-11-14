package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
)

func TestOrganizationDataSource(t *testing.T) {
	runLookupDataSourceTests(t, lookupDataSourceCase{
		apiPath:    "/api/v1/organization",
		filterKey:  "filter[organization]",
		entityType: "organization",
		config:     map[string]string{},
		matchAttrs: `"description":"Main org","executionMode":"remote"`,
		wantState:  map[string]string{"id": "obj-1", "description": "Main org", "execution_mode": "remote"},
		notFound:   "Organization not found",
		multiple:   "Multiple organization entries found",
		apiError:   "Request failed",
		badPayload: "Unable to unmarshal payload",
		newSource: func(c *http.Client, endpoint string) datasource.DataSource {
			return &OrganizationDataSource{client: c, endpoint: endpoint, token: "test-token"}
		},
	})
}
