package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Terrakube cannot delete an organization, so the resource disables it with a PATCH.
func TestOrganizationResource(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	runCrudResourceTests(t, crudResourceCase{
		itemType:   "organization",
		collection: "/api/v1/organization",
		newSource: func(c *http.Client, endpoint string) resource.Resource {
			return &OrganizationResource{client: c, endpoint: endpoint, token: "test-token"}
		},
		noOrganization: true,
		create: map[string]tftypes.Value{
			"name":           str("platform"),
			"description":    str("Platform engineering"),
			"execution_mode": str("remote"),
			"icon":           str("cloud"),
		},
		wantSent:      map[string]any{"name": "platform", "description": "Platform engineering", "executionMode": "remote", "icon": "cloud"},
		wantCreated:   map[string]string{"name": "platform", "description": "Platform engineering", "execution_mode": "remote", "icon": "cloud"},
		drift:         map[string]any{"executionMode": "local"},
		wantDrifted:   map[string]string{"execution_mode": "local", "name": "platform"},
		update:        map[string]tftypes.Value{"description": str("Platform and SRE")},
		wantSentPatch: map[string]any{"description": "Platform and SRE"},
		wantUpdated:   map[string]string{"description": "Platform and SRE"},

		deleteMethod:      http.MethodPatch,
		wantSentDelete:    map[string]any{"disabled": true},
		deleteMissingIsOK: true,

		importID:     "item-9",
		wantImported: map[string]string{"id": "item-9"},
	})
}
