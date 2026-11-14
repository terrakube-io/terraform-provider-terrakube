package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
)

func TestOrganizationTemplateDataSource(t *testing.T) {
	runLookupDataSourceTests(t, lookupDataSourceCase{
		apiPath:    "/api/v1/organization/org-1/template",
		filterKey:  "filter[template]",
		entityType: "template",
		config:     map[string]string{"organization_id": "org-1"},
		matchAttrs: ``,
		wantState:  map[string]string{"id": "obj-1"},
		notFound:   "Organization template not found",
		multiple:   "Multiple organization template entries found",
		apiError:   "Error reading organization template: 403 Forbidden",
		badPayload: "Unable to unmarshal payload",
		newSource: func(c *http.Client, endpoint string) datasource.DataSource {
			return &OrganizationTemplateDataSource{client: c, endpoint: endpoint, token: "test-token"}
		},
	})
}
