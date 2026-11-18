package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestFederatedCredentialClaimResource(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	runCrudResourceTests(t, crudResourceCase{
		itemType:   "federated_claim",
		collection: "/api/v1/federated/fc-1/claims",
		newSource: func(c *http.Client, endpoint string) resource.Resource {
			return &FederatedCredentialClaimResource{client: c, endpoint: endpoint, token: "test-token"}
		},
		noOrganization: true,
		scope:          map[string]tftypes.Value{"federated_credential_id": str("fc-1")},
		create: map[string]tftypes.Value{
			"claim_key":   str("repository"),
			"claim_value": str("terrakube-io/terraform-provider-terrakube"),
		},
		wantSent:      map[string]any{"claimKey": "repository", "claimValue": "terrakube-io/terraform-provider-terrakube"},
		wantCreated:   map[string]string{"claim_key": "repository", "claim_value": "terrakube-io/terraform-provider-terrakube", "federated_credential_id": "fc-1"},
		drift:         map[string]any{"claimValue": "terrakube-io/other"},
		wantDrifted:   map[string]string{"claim_value": "terrakube-io/other", "claim_key": "repository"},
		update:        map[string]tftypes.Value{"claim_value": str("terrakube-io/terrakube")},
		wantSentPatch: map[string]any{"claimValue": "terrakube-io/terrakube"},
		wantUpdated:   map[string]string{"claim_value": "terrakube-io/terrakube"},

		deleteMissingIsOK: true,

		importID:     "fc-9,item-9",
		wantImported: map[string]string{"federated_credential_id": "fc-9", "id": "item-9"},
		badImportIDs: []string{"item-9", "fc-9,", ",item-9", "fc-9,item-9,extra", ""},
	})
}
