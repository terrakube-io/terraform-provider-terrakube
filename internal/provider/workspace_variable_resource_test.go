package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestWorkspaceVariableResource(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	flag := func(v bool) tftypes.Value { return tftypes.NewValue(tftypes.Bool, v) }
	runCrudResourceTests(t, crudResourceCase{
		itemType:   "variable",
		collection: "/api/v1/organization/org-1/workspace/ws-1/variable",
		newSource: func(c *http.Client, endpoint string) resource.Resource {
			return &WorkspaceVariableResource{client: c, endpoint: endpoint, token: "test-token"}
		},
		scope: map[string]tftypes.Value{"workspace_id": str("ws-1")},
		create: map[string]tftypes.Value{
			"key":         str("instance_type"),
			"value":       str("t3.small"),
			"description": str("Instance type"),
			"category":    str("TERRAFORM"),
			"sensitive":   flag(false),
			"hcl":         flag(false),
		},
		wantSent: map[string]any{
			"key": "instance_type", "value": "t3.small", "description": "Instance type",
			"category": "TERRAFORM", "sensitive": false, "hcl": false,
		},
		wantCreated:       map[string]string{"key": "instance_type", "value": "t3.small", "category": "TERRAFORM", "workspace_id": "ws-1"},
		drift:             map[string]any{"value": "t3.large"},
		wantDrifted:       map[string]string{"value": "t3.large", "key": "instance_type"},
		update:            map[string]tftypes.Value{"value": str("t3.medium")},
		wantSentPatch:     map[string]any{"value": "t3.medium"},
		deleteMissingIsOK: true,
		wantUpdated:       map[string]string{"value": "t3.medium"},

		importID:     "org-9,ws-9,item-9",
		wantImported: map[string]string{"organization_id": "org-9", "workspace_id": "ws-9", "id": "item-9"},
		badImportIDs: []string{"item-9", "org-9,ws-9", "org-9,,item-9", ",ws-9,item-9", "org-9,ws-9,", "org-9,ws-9,item-9,extra", ""},
	})
}
