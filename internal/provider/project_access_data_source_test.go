package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
)

func TestProjectAccessDataSource(t *testing.T) {
	runAccessDataSourceTests(t, accessDataSourceCase{
		parent:     "project",
		apiPath:    "/api/v1/organization/org-1/project/project-1/projectAccess",
		filterKey:  "filter[project_access]",
		entityType: "project_access",
		notFound:   "Project access not found",
		multiple:   "Multiple project access entries found",
		apiError:   "Error reading project access: 403 Forbidden",
		badPayload: "Unable to unmarshal project access payload",
		newSource: func(c *http.Client, endpoint string) datasource.DataSource {
			return &ProjectAccessDataSource{client: c, endpoint: endpoint, token: "test-token"}
		},
	})
}
