package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Acceptance tests for the organization-level data sources. Each reads back an object its
// configuration in testdata/<TestName>/main.tf creates.

func TestAccOrganizationDataSource(t *testing.T) {
	testAccDataSource(t, testAccCheckSameAttrs("data.terrakube_organization.test", "terrakube_organization.test",
		"id", "name", "description", "execution_mode"))
}

func TestAccOrganizationTagDataSource(t *testing.T) {
	testAccDataSource(t, testAccCheckSameAttrs("data.terrakube_organization_tag.test", "terrakube_organization_tag.test",
		"id", "name", "organization_id"))
}

func TestAccOrganizationTemplateDataSource(t *testing.T) {
	testAccDataSource(t, testAccCheckSameAttrs("data.terrakube_organization_template.test", "terrakube_organization_template.test",
		"id", "name", "organization_id"))
}

func TestAccOrganizationVariableDataSource(t *testing.T) {
	testAccDataSource(t, testAccCheckSameAttrs("data.terrakube_organization_variable.test", "terrakube_organization_variable.test",
		"id", "organization_id", "key", "value", "description", "category", "sensitive", "hcl"))
}

func TestAccProjectDataSource(t *testing.T) {
	testAccDataSource(t, resource.ComposeAggregateTestCheckFunc(
		testAccCheckSameAttrs("data.terrakube_project.test", "terrakube_project.test", "id", "name", "description"),
		resource.TestCheckResourceAttrPair("data.terrakube_project.test", "organization", "terrakube_organization.test", "name"),
	))
}

func TestAccTeamDataSource(t *testing.T) {
	testAccDataSource(t, resource.ComposeAggregateTestCheckFunc(
		testAccCheckSameAttrs("data.terrakube_team.test", "terrakube_team.admins",
			"name", "manage_workspace", "manage_template", "manage_collection", "manage_vcs",
			"manage_module", "manage_provider", "manage_state", "manage_job"),
		resource.TestCheckResourceAttrPair("data.terrakube_team.test", "organization", "terrakube_organization.test", "name"),
	))
}
