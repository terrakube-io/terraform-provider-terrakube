package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestFederatedCredentialResource(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	runCrudResourceTests(t, crudResourceCase{
		itemType:   "federated",
		collection: "/api/v1/federated",
		newSource: func(c *http.Client, endpoint string) resource.Resource {
			return &FederatedCredentialResource{client: c, endpoint: endpoint, token: "test-token"}
		},
		noOrganization: true,
		create: map[string]tftypes.Value{
			"name":       str("github-actions"),
			"issuer_url": str("https://token.actions.githubusercontent.com"),
			"audience":   str("terrakube"),
		},
		wantSent:      map[string]any{"name": "github-actions", "issuerUrl": "https://token.actions.githubusercontent.com", "audience": "terrakube"},
		wantCreated:   map[string]string{"name": "github-actions", "issuer_url": "https://token.actions.githubusercontent.com", "audience": "terrakube"},
		drift:         map[string]any{"audience": "changed-outside"},
		wantDrifted:   map[string]string{"audience": "changed-outside", "name": "github-actions"},
		update:        map[string]tftypes.Value{"audience": str("terrakube-prod")},
		wantSentPatch: map[string]any{"audience": "terrakube-prod"},
		wantUpdated:   map[string]string{"audience": "terrakube-prod"},

		deleteMissingIsOK: true,

		// The import ID is the credential ID alone.
		importID:     "item-9",
		wantImported: map[string]string{"id": "item-9"},
	})
}
