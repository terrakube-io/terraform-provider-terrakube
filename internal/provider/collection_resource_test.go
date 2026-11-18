package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestCollectionResource(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	num := func(v int64) tftypes.Value { return tftypes.NewValue(tftypes.Number, v) }
	runCrudResourceTests(t, crudResourceCase{
		itemType:   "collection",
		collection: "/api/v1/organization/org-1/collection",
		newSource: func(c *http.Client, endpoint string) resource.Resource {
			return &CollectionResource{client: c, endpoint: endpoint, token: "test-token"}
		},
		create: map[string]tftypes.Value{
			"name":        str("shared-settings"),
			"description": str("Settings shared by every workspace"),
			"priority":    num(10),
		},
		wantSent:          map[string]any{"name": "shared-settings", "description": "Settings shared by every workspace", "priority": 10},
		wantCreated:       map[string]string{"name": "shared-settings", "description": "Settings shared by every workspace", "priority": "10"},
		drift:             map[string]any{"priority": 20},
		wantDrifted:       map[string]string{"priority": "20", "name": "shared-settings"},
		update:            map[string]tftypes.Value{"priority": num(30), "description": str("Updated")},
		wantSentPatch:     map[string]any{"priority": 30, "description": "Updated"},
		deleteMissingIsOK: true,
		wantUpdated:       map[string]string{"priority": "30", "description": "Updated"},
	})
}
