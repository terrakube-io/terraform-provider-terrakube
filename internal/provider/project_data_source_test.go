package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
)

func TestProjectDataSource(t *testing.T) {
	runScopedDataSourceTests(t, scopedDataSourceCase{
		entityPath: "/api/v1/organization/org-1/project",
		filterKey:  "filter[project]",
		entityType: "project",
		matchAttrs: `"description":"Platform workspaces"`,
		wantState:  map[string]string{"id": "obj-1", "description": "Platform workspaces"},
		nameAttr:   "name",
		notFound:   "Project not found",
		multiple:   "Multiple project entries found",
		apiError:   "Error reading project: 403 Forbidden",
		newSource: func(c *http.Client, endpoint string) datasource.DataSource {
			return &ProjectDataSource{client: c, endpoint: endpoint, token: "test-token"}
		},
	})
}
