package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
)

func TestWorkspaceAccessDataSource(t *testing.T) {
	runAccessDataSourceTests(t, accessDataSourceCase{
		parent:     "workspace",
		apiPath:    "/api/v1/organization/org-1/workspace/workspace-1/access",
		filterKey:  "filter[access]",
		entityType: "access",
		notFound:   "Workspace access not found",
		multiple:   "Multiple workspace access entries found",
		apiError:   "Error reading workspace access: 403 Forbidden",
		badPayload: "Unable to unmarshal workspace access payload",
		newSource: func(c *http.Client, endpoint string) datasource.DataSource {
			return &WorkspaceAccessDataSource{client: c, endpoint: endpoint, token: "test-token"}
		},
	})
}
