package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/config"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Configuration: testdata/TestAccWorkspaceRunTriggerResource/main.tf.
func TestAccWorkspaceRunTriggerResource(t *testing.T) {
	// Run triggers were added in Terrakube 2.34.0.
	testAccRequireTerrakube(t, "2.34.0")
	name := "acc-" + acctest.RandString(8)
	variables := func(enabled bool) config.Variables {
		return config.Variables{
			"name":    config.StringVariable(name),
			"enabled": config.BoolVariable(enabled),
		}
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				ConfigDirectory: config.TestNameDirectory(),
				ConfigVariables: variables(true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("terrakube_workspace_run_trigger.test", "id"),
					resource.TestCheckResourceAttrPair("terrakube_workspace_run_trigger.test", "source_workspace_id", "terrakube_workspace_cli.upstream", "id"),
					resource.TestCheckResourceAttrPair("terrakube_workspace_run_trigger.test", "destination_workspace_id", "terrakube_workspace_cli.downstream", "id"),
					resource.TestCheckResourceAttr("terrakube_workspace_run_trigger.test", "enabled", "true"),
				),
			},
			{
				ConfigDirectory:   config.TestNameDirectory(),
				ConfigVariables:   variables(true),
				ResourceName:      "terrakube_workspace_run_trigger.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ConfigDirectory: config.TestNameDirectory(),
				ConfigVariables: variables(false),
				Check:           resource.TestCheckResourceAttr("terrakube_workspace_run_trigger.test", "enabled", "false"),
			},
		},
	})
}
