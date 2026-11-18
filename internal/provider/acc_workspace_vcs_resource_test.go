package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/config"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Configuration: testdata/TestAccWorkspaceVcsResource/main.tf.
func TestAccWorkspaceVcsResource(t *testing.T) {
	name := "acc-" + acctest.RandString(8)
	variables := func(description, branch, folder string) config.Variables {
		return config.Variables{
			"name":        config.StringVariable(name),
			"description": config.StringVariable(description),
			"branch":      config.StringVariable(branch),
			"folder":      config.StringVariable(folder),
		}
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			testAccCheckDestroyed("terrakube_workspace_vcs", testAccWorkspaceGone),
			testAccCheckDestroyed("terrakube_organization", testAccOrganizationGone),
		),
		Steps: []resource.TestStep{
			{
				ConfigDirectory: config.TestNameDirectory(),
				ConfigVariables: variables("first description", "main", "/"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("terrakube_workspace_vcs.test", "id"),
					resource.TestCheckResourceAttr("terrakube_workspace_vcs.test", "name", name),
					resource.TestCheckResourceAttr("terrakube_workspace_vcs.test", "description", "first description"),
					resource.TestCheckResourceAttr("terrakube_workspace_vcs.test", "repository", "https://github.com/example/infrastructure.git"),
					resource.TestCheckResourceAttr("terrakube_workspace_vcs.test", "branch", "main"),
					resource.TestCheckResourceAttr("terrakube_workspace_vcs.test", "folder", "/"),
					resource.TestCheckResourceAttrPair("terrakube_workspace_vcs.test", "template_id", "terrakube_organization_template.plan", "id"),
				),
			},
			{
				ConfigDirectory:   config.TestNameDirectory(),
				ConfigVariables:   variables("first description", "main", "/"),
				ResourceName:      "terrakube_workspace_vcs.test",
				ImportState:       true,
				ImportStateIdFunc: testAccImportID("terrakube_workspace_vcs.test", "organization_id"),
				ImportStateVerify: true,
			},
			{
				ConfigDirectory: config.TestNameDirectory(),
				ConfigVariables: variables("second description", "develop", "/environments/dev"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("terrakube_workspace_vcs.test", "description", "second description"),
					resource.TestCheckResourceAttr("terrakube_workspace_vcs.test", "branch", "develop"),
					resource.TestCheckResourceAttr("terrakube_workspace_vcs.test", "folder", "/environments/dev"),
				),
			},
		},
	})
}
