package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
)

func TestFederatedCredentialDataSource(t *testing.T) {
	runLookupDataSourceTests(t, lookupDataSourceCase{
		apiPath:    "/api/v1/federated",
		filterKey:  "filter[federated]",
		entityType: "federated",
		matchAttrs: `"issuerUrl":"https://issuer.example.com","audience":"terrakube"`,
		wantState:  map[string]string{"id": "obj-1", "issuer_url": "https://issuer.example.com", "audience": "terrakube"},
		notFound:   "Federated credential not found",
		multiple:   "Multiple federated credential entries found",
		apiError:   "Error reading federated credential: 403 Forbidden",
		badPayload: "Unable to unmarshal payload",
		newSource: func(c *http.Client, endpoint string) datasource.DataSource {
			return &FederatedCredentialDataSource{client: c, endpoint: endpoint, token: "test-token"}
		},
	})
}
