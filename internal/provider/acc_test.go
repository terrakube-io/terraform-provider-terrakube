package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

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
