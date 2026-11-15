package provider

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/config"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// testAccDataSource runs a single-step test for a data source. The configuration in
// testdata/<TestName>/main.tf creates the object with its resource and reads it back with the
// data source; it gets a random var.name to keep tests apart. check usually compares the two
// with testAccCheckSameAttrs.
func testAccDataSource(t *testing.T, check resource.TestCheckFunc) {
	t.Helper()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				ConfigDirectory: config.TestNameDirectory(),
				ConfigVariables: config.Variables{"name": config.StringVariable("acc-" + acctest.RandString(8))},
				Check:           check,
			},
		},
	})
}

// testAccCheckSameAttrs checks that the data source reads the same value as the resource for each
// attribute, where both use the same attribute name.
func testAccCheckSameAttrs(dataSource, resourceName string, attrs ...string) resource.TestCheckFunc {
	checks := make([]resource.TestCheckFunc, 0, len(attrs))
	for _, attr := range attrs {
		checks = append(checks, resource.TestCheckResourceAttrPair(dataSource, attr, resourceName, attr))
	}
	return resource.ComposeAggregateTestCheckFunc(checks...)
}

// testAccProtoV6ProviderFactories runs the provider in-process for acceptance tests.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"terrakube": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccPreCheck stops an acceptance test early when the Terrakube instance is not configured.
// Acceptance tests run only with TF_ACC=1 and need a real Terrakube API.
func testAccPreCheck(t *testing.T) {
	t.Helper()
	for _, name := range []string{"TERRAKUBE_ENDPOINT", "TERRAKUBE_TOKEN"} {
		if os.Getenv(name) == "" {
			t.Fatalf("%s must be set for acceptance tests", name)
		}
	}
}

// testAccRequireTerrakube skips the test when TERRAKUBE_VERSION names a Terrakube release older
// than minimum. Use it for a resource or attribute that needs a newer server than the rest. The
// Terrakube API does not report its version, so the test run sets TERRAKUBE_VERSION
// (scripts/acceptance.sh and the Acceptance workflow do). When it is unset, the test runs.
func testAccRequireTerrakube(t *testing.T, minimum string) {
	t.Helper()
	current := os.Getenv("TERRAKUBE_VERSION")
	if current == "" {
		return
	}
	older, err := terrakubeVersionOlder(current, minimum)
	if err != nil {
		t.Fatalf("TERRAKUBE_VERSION: %s", err)
	}
	if older {
		t.Skipf("needs Terrakube %s or later, running against %s", minimum, current)
	}
}

// terrakubeVersionOlder reports whether version a is older than b. Both are MAJOR.MINOR.PATCH,
// optionally with a leading "v"; a suffix such as "-arm" or "-beta.1" on a is ignored.
func terrakubeVersionOlder(a, b string) (bool, error) {
	pa, err := parseTerrakubeVersion(a)
	if err != nil {
		return false, err
	}
	pb, err := parseTerrakubeVersion(b)
	if err != nil {
		return false, err
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] < pb[i], nil
		}
	}
	return false, nil
}

func parseTerrakubeVersion(version string) ([3]int, error) {
	var parts [3]int
	core, _, _ := strings.Cut(strings.TrimPrefix(version, "v"), "-")
	fields := strings.Split(core, ".")
	if len(fields) != 3 {
		return parts, fmt.Errorf("%q is not MAJOR.MINOR.PATCH", version)
	}
	for i, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil || n < 0 {
			return parts, fmt.Errorf("%q is not MAJOR.MINOR.PATCH", version)
		}
		parts[i] = n
	}
	return parts, nil
}
