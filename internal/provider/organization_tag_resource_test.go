package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestOrganizationTagResource(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	runCrudResourceTests(t, crudResourceCase{
		itemType:   "tag",
		collection: "/api/v1/organization/org-1/tag",
		newSource: func(c *http.Client, endpoint string) resource.Resource {
			return &OrganizationTagResource{client: newTestClient(c, endpoint)}
		},
		create:        map[string]tftypes.Value{"name": str("production")},
		wantSent:      map[string]any{"name": "production"},
		wantCreated:   map[string]string{"name": "production"},
		drift:         map[string]any{"name": "renamed-outside"},
		wantDrifted:   map[string]string{"name": "renamed-outside"},
		update:        map[string]tftypes.Value{"name": str("staging")},
		wantSentPatch: map[string]any{"name": "staging"},
		wantUpdated:   map[string]string{"name": "staging"},

		deleteMissingIsOK: true,
	})
}
