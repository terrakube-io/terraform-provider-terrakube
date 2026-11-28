package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestWorkspaceAccessResource(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	flag := func(v bool) tftypes.Value { return tftypes.NewValue(tftypes.Bool, v) }
	runCrudResourceTests(t, crudResourceCase{
		itemType:   "access",
		collection: "/api/v1/organization/org-1/workspace/workspace-1/access",
		newSource: func(c *http.Client, endpoint string) resource.Resource {
			return &WorkspaceAccessResource{client: newTestClient(c, endpoint)}
		},
		scope: map[string]tftypes.Value{"workspace_id": str("workspace-1")},
		create: map[string]tftypes.Value{
			"name":             str("platform-team"),
			"role":             str("custom"),
			"manage_state":     flag(true),
			"manage_job":       flag(false),
			"manage_workspace": flag(true),
			"plan_job":         flag(true),
			"approve_job":      flag(false),
			"destroy_job":      flag(false),
		},
		wantSent: map[string]any{
			"name": "platform-team", "role": "custom", "manageState": true, "manageJob": false,
			"manageWorkspace": true, "planJob": true, "approveJob": false, "destroyJob": false,
		},
		wantCreated:   map[string]string{"name": "platform-team", "role": "custom", "manage_state": "true", "manage_job": "false", "workspace_id": "workspace-1"},
		drift:         map[string]any{"manageState": false},
		wantDrifted:   map[string]string{"manage_state": "false", "name": "platform-team"},
		update:        map[string]tftypes.Value{"manage_job": flag(true), "approve_job": flag(true)},
		wantSentPatch: map[string]any{"manageJob": true, "approveJob": true},
		wantUpdated:   map[string]string{"manage_job": "true", "approve_job": "true"},

		importID:     "org-9,workspace-9,item-9",
		wantImported: map[string]string{"organization_id": "org-9", "workspace_id": "workspace-9", "id": "item-9"},
		badImportIDs: []string{"item-9", "org-9,workspace-9", "org-9,,item-9", ",workspace-9,item-9", "org-9,workspace-9,", "org-9,workspace-9,item-9,extra", ""},
	})
}
