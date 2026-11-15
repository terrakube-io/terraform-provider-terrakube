package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccOrganizationResource(t *testing.T) {
	name := "acc-" + acctest.RandString(8)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccOrganizationConfig(name, "first description", "local"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("terrakube_organization.test", "id"),
					resource.TestCheckResourceAttr("terrakube_organization.test", "name", name),
					resource.TestCheckResourceAttr("terrakube_organization.test", "description", "first description"),
					resource.TestCheckResourceAttr("terrakube_organization.test", "execution_mode", "local"),
				),
			},
			{
				ResourceName:      "terrakube_organization.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccOrganizationConfig(name, "second description", "remote"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("terrakube_organization.test", "description", "second description"),
					resource.TestCheckResourceAttr("terrakube_organization.test", "execution_mode", "remote"),
				),
			},
		},
	})
}

func testAccOrganizationConfig(name, description, executionMode string) string {
	return fmt.Sprintf(`
resource "terrakube_organization" "test" {
  name           = %q
  description    = %q
  execution_mode = %q
}
`, name, description, executionMode)
}
