package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/config"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
)

// testAccVariableSteps creates a plain and a sensitive variable, imports both, then changes their
// value and description. Terrakube never returns a sensitive value, so the import of the sensitive
// variable can't verify it.
func testAccVariableSteps(resourceType string, importAttrs ...string) []resource.TestStep {
	name := "acc-" + acctest.RandString(8)
	variables := func(value, description string) config.Variables {
		return config.Variables{
			"name":        config.StringVariable(name),
			"value":       config.StringVariable(value),
			"description": config.StringVariable(description),
		}
	}
	plain, secret := resourceType+".test", resourceType+".secret"
	return []resource.TestStep{
		{
			ConfigDirectory: config.TestNameDirectory(),
			ConfigVariables: variables("first-value", "first description"),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(plain, "id"),
				resource.TestCheckResourceAttr(plain, "value", "first-value"),
				resource.TestCheckResourceAttr(plain, "description", "first description"),
				resource.TestCheckResourceAttr(plain, "sensitive", "false"),
				resource.TestCheckResourceAttr(secret, "value", "first-value"),
				resource.TestCheckResourceAttr(secret, "sensitive", "true"),
			),
		},
		{
			ConfigDirectory:   config.TestNameDirectory(),
			ConfigVariables:   variables("first-value", "first description"),
			ResourceName:      plain,
			ImportState:       true,
			ImportStateIdFunc: testAccImportID(plain, importAttrs...),
			ImportStateVerify: true,
		},
		{
			ConfigDirectory:         config.TestNameDirectory(),
			ConfigVariables:         variables("first-value", "first description"),
			ResourceName:            secret,
			ImportState:             true,
			ImportStateIdFunc:       testAccImportID(secret, importAttrs...),
			ImportStateVerify:       true,
			ImportStateVerifyIgnore: []string{"value"},
		},
		{
			ConfigDirectory: config.TestNameDirectory(),
			ConfigVariables: variables("second-value", "second description"),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(plain, "value", "second-value"),
				resource.TestCheckResourceAttr(plain, "description", "second description"),
				resource.TestCheckResourceAttr(secret, "value", "second-value"),
				resource.TestCheckResourceAttr(secret, "description", "second description"),
			),
		},
	}
}

// Configuration: testdata/TestAccOrganizationVariableResource/main.tf.
func TestAccOrganizationVariableResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccOrganizationVariableDestroyed,
		Steps:                    testAccVariableSteps("terrakube_organization_variable", "organization_id"),
	})
}

// Configuration: testdata/TestAccWorkspaceVariableResource/main.tf.
func TestAccWorkspaceVariableResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccWorkspaceVariableDestroyed,
		Steps:                    testAccVariableSteps("terrakube_workspace_variable", "organization_id", "workspace_id"),
	})
}

var testAccOrganizationVariableDestroyed = resource.ComposeAggregateTestCheckFunc(
	testAccCheckDestroyed("terrakube_organization_variable", testAccGoneWhenNotFound(func(rs *terraform.ResourceState) string {
		return "/api/v1/organization/" + rs.Primary.Attributes["organization_id"] + "/globalvar/" + rs.Primary.ID
	})),
	testAccCheckDestroyed("terrakube_organization", testAccOrganizationGone),
)

var testAccWorkspaceVariableDestroyed = resource.ComposeAggregateTestCheckFunc(
	testAccCheckDestroyed("terrakube_workspace_variable", testAccGoneWhenNotFound(func(rs *terraform.ResourceState) string {
		return "/api/v1/organization/" + rs.Primary.Attributes["organization_id"] + "/workspace/" +
			rs.Primary.Attributes["workspace_id"] + "/variable/" + rs.Primary.ID
	})),
	testAccCheckDestroyed("terrakube_workspace_cli", testAccWorkspaceGone),
	testAccCheckDestroyed("terrakube_organization", testAccOrganizationGone),
)

// testAccWriteOnlyVariableSteps creates a variable with value_wo, which must be sensitive. Terrakube
// never returns a sensitive value, so the steps check what Terraform does instead: neither value
// nor value_wo is stored in state, a new value_wo alone changes nothing, and a new
// value_wo_version updates the variable.
func testAccWriteOnlyVariableSteps(resourceName string) []resource.TestStep {
	name := "acc-" + acctest.RandString(8)
	variables := func(value string, version int) config.Variables {
		return config.Variables{
			"name":          config.StringVariable(name),
			"value":         config.StringVariable(value),
			"value_version": config.IntegerVariable(version),
		}
	}
	return []resource.TestStep{
		{
			ConfigDirectory: config.TestNameDirectory(),
			ConfigVariables: variables("first-value", 1),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet(resourceName, "id"),
				resource.TestCheckResourceAttr(resourceName, "sensitive", "true"),
				resource.TestCheckResourceAttr(resourceName, "value_wo_version", "1"),
				resource.TestCheckNoResourceAttr(resourceName, "value"),
				resource.TestCheckNoResourceAttr(resourceName, "value_wo"),
			),
		},
		{
			ConfigDirectory: config.TestNameDirectory(),
			ConfigVariables: variables("second-value", 1),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
		},
		{
			ConfigDirectory: config.TestNameDirectory(),
			ConfigVariables: variables("second-value", 2),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(resourceName, plancheck.ResourceActionUpdate)},
			},
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(resourceName, "value_wo_version", "2"),
				resource.TestCheckNoResourceAttr(resourceName, "value"),
				resource.TestCheckNoResourceAttr(resourceName, "value_wo"),
			),
		},
	}
}

// Write-only arguments need Terraform 1.11 or OpenTofu 1.11, so older CLIs skip these tests.
var testAccWriteOnlyVersionChecks = []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_11_0)}

// Configuration: testdata/TestAccOrganizationVariableResource_writeOnly/main.tf.
func TestAccOrganizationVariableResource_writeOnly(t *testing.T) {
	resource.Test(t, resource.TestCase{
		TerraformVersionChecks:   testAccWriteOnlyVersionChecks,
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccOrganizationVariableDestroyed,
		Steps:                    testAccWriteOnlyVariableSteps("terrakube_organization_variable.test"),
	})
}

// Configuration: testdata/TestAccWorkspaceVariableResource_writeOnly/main.tf.
func TestAccWorkspaceVariableResource_writeOnly(t *testing.T) {
	resource.Test(t, resource.TestCase{
		TerraformVersionChecks:   testAccWriteOnlyVersionChecks,
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccWorkspaceVariableDestroyed,
		Steps:                    testAccWriteOnlyVariableSteps("terrakube_workspace_variable.test"),
	})
}
