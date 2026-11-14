package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
)

func TestTeamDataSource(t *testing.T) {
	runScopedDataSourceTests(t, scopedDataSourceCase{
		entityPath: "/api/v1/organization/org-1/team",
		filterKey:  "filter[team]",
		entityType: "team",
		matchAttrs: `"role":"write","manageState":true`,
		wantState:  map[string]string{"role": "write"},
		nameAttr:   "name",
		notFound:   "Team not found",
		multiple:   "Multiple team entries found",
		apiError:   "Error reading team: 403 Forbidden",
		newSource: func(c *http.Client, endpoint string) datasource.DataSource {
			return &TeamDataSource{client: c, endpoint: endpoint, token: "test-token"}
		},
	})
}
