package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/config"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Configuration: testdata/TestAccOrganizationResource/main.tf.
func TestAccOrganizationResource(t *testing.T) {
	name := "acc-" + acctest.RandString(8)
	variables := func(description, executionMode string) config.Variables {
		return config.Variables{
			"name":           config.StringVariable(name),
			"description":    config.StringVariable(description),
			"execution_mode": config.StringVariable(executionMode),
		}
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				ConfigDirectory: config.TestNameDirectory(),
				ConfigVariables: variables("first description", "local"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("terrakube_organization.test", "id"),
					resource.TestCheckResourceAttr("terrakube_organization.test", "name", name),
					resource.TestCheckResourceAttr("terrakube_organization.test", "description", "first description"),
					resource.TestCheckResourceAttr("terrakube_organization.test", "execution_mode", "local"),
				),
			},
			{
				ConfigDirectory:   config.TestNameDirectory(),
				ConfigVariables:   variables("first description", "local"),
				ResourceName:      "terrakube_organization.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ConfigDirectory: config.TestNameDirectory(),
				ConfigVariables: variables("second description", "remote"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("terrakube_organization.test", "description", "second description"),
					resource.TestCheckResourceAttr("terrakube_organization.test", "execution_mode", "remote"),
				),
			},
		},
	})
}
