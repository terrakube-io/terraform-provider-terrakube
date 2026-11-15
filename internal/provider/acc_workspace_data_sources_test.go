package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Acceptance tests for the workspace and access data sources. Each reads back an object its
// configuration in testdata/<TestName>/main.tf creates.

func TestAccWorkspaceDataSource(t *testing.T) {
	testAccDataSource(t, resource.ComposeAggregateTestCheckFunc(
		testAccCheckSameAttrs("data.terrakube_workspace.test", "terrakube_workspace_cli.test",
			"id", "name", "description", "organization_id"),
		resource.TestCheckResourceAttrPair("data.terrakube_workspace.test", "organization", "terrakube_organization.test", "name"),
		resource.TestCheckResourceAttr("data.terrakube_workspace.test", "iactype", "tofu"),
		resource.TestCheckResourceAttr("data.terrakube_workspace.test", "iacversion", "1.12.5"),
		resource.TestCheckResourceAttr("data.terrakube_workspace.test", "executionmode", "local"),
	))
}

// A key-only tag, which every supported Terrakube version has.
func TestAccWorkspaceTagDataSource(t *testing.T) {
	testAccDataSource(t, resource.ComposeAggregateTestCheckFunc(
		testAccCheckSameAttrs("data.terrakube_workspace_tag.test", "terrakube_workspace_tag.test",
			"id", "organization_id", "workspace_id", "tag_id"),
		resource.TestCheckResourceAttrPair("data.terrakube_workspace_tag.test", "name", "terrakube_organization_tag.test", "name"),
		resource.TestCheckNoResourceAttr("data.terrakube_workspace_tag.test", "value"),
	))
}

// A key/value tag. Terrakube 2.33 rejects the value attribute when the tag is created.
func TestAccWorkspaceTagDataSource_value(t *testing.T) {
	testAccRequireTerrakube(t, "2.34.0")
	testAccDataSource(t, resource.ComposeAggregateTestCheckFunc(
		testAccCheckSameAttrs("data.terrakube_workspace_tag.test", "terrakube_workspace_tag.test",
			"id", "organization_id", "workspace_id", "tag_id", "value"),
		resource.TestCheckResourceAttr("data.terrakube_workspace_tag.test", "value", "acceptance"),
	))
}

func TestAccWorkspaceVariableDataSource(t *testing.T) {
	testAccDataSource(t, testAccCheckSameAttrs("data.terrakube_workspace_variable.test", "terrakube_workspace_variable.test",
		"id", "organization_id", "workspace_id", "key", "value", "description", "category", "sensitive", "hcl"))
}

func TestAccWorkspaceAccessDataSource(t *testing.T) {
	testAccDataSource(t, testAccCheckSameAttrs("data.terrakube_workspace_access.test", "terrakube_workspace_access.test",
		"id", "organization_id", "workspace_id", "name", "manage_state", "manage_job", "manage_workspace"))
}

func TestAccProjectAccessDataSource(t *testing.T) {
	testAccDataSource(t, testAccCheckSameAttrs("data.terrakube_project_access.test", "terrakube_project_access.test",
		"id", "organization_id", "project_id", "name", "manage_state", "manage_job", "manage_workspace"))
}
