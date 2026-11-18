package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/config"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Configuration: testdata/TestAccWorkspaceCliResource/main.tf.
func TestAccWorkspaceCliResource(t *testing.T) {
	name := "acc-" + acctest.RandString(8)
	variables := func(description, iacVersion, executionMode string) config.Variables {
		return config.Variables{
			"name":           config.StringVariable(name),
			"description":    config.StringVariable(description),
			"iac_version":    config.StringVariable(iacVersion),
			"execution_mode": config.StringVariable(executionMode),
		}
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			testAccCheckDestroyed("terrakube_workspace_cli", testAccWorkspaceGone),
			testAccCheckDestroyed("terrakube_organization", testAccOrganizationGone),
		),
		Steps: []resource.TestStep{
			{
				ConfigDirectory: config.TestNameDirectory(),
				ConfigVariables: variables("first description", "1.12.5", "local"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("terrakube_workspace_cli.test", "id"),
					resource.TestCheckResourceAttrPair("terrakube_workspace_cli.test", "organization_id", "terrakube_organization.test", "id"),
					resource.TestCheckResourceAttr("terrakube_workspace_cli.test", "name", name),
					resource.TestCheckResourceAttr("terrakube_workspace_cli.test", "description", "first description"),
					resource.TestCheckResourceAttr("terrakube_workspace_cli.test", "execution_mode", "local"),
					resource.TestCheckResourceAttr("terrakube_workspace_cli.test", "iac_type", "tofu"),
					resource.TestCheckResourceAttr("terrakube_workspace_cli.test", "iac_version", "1.12.5"),
				),
			},
			{
				ConfigDirectory:   config.TestNameDirectory(),
				ConfigVariables:   variables("first description", "1.12.5", "local"),
				ResourceName:      "terrakube_workspace_cli.test",
				ImportState:       true,
				ImportStateIdFunc: testAccImportID("terrakube_workspace_cli.test", "organization_id"),
				ImportStateVerify: true,
			},
			{
				ConfigDirectory: config.TestNameDirectory(),
				ConfigVariables: variables("second description", "1.11.6", "remote"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("terrakube_workspace_cli.test", "description", "second description"),
					resource.TestCheckResourceAttr("terrakube_workspace_cli.test", "iac_version", "1.11.6"),
					resource.TestCheckResourceAttr("terrakube_workspace_cli.test", "execution_mode", "remote"),
				),
			},
		},
	})
}
