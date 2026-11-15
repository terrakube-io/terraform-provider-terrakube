package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestProjectResource(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	runCrudResourceTests(t, crudResourceCase{
		itemType:   "project",
		collection: "/project",
		newSource: func(c *http.Client, endpoint string) resource.Resource {
			return &ProjectResource{client: c, endpoint: endpoint, token: "test-token"}
		},
		create: map[string]tftypes.Value{
			"name":        str("platform"),
			"description": str("Platform workspaces"),
		},
		wantSent:      map[string]any{"name": "platform", "description": "Platform workspaces"},
		wantCreated:   map[string]string{"name": "platform", "description": "Platform workspaces"},
		drift:         map[string]any{"description": "changed outside Terraform"},
		wantDrifted:   map[string]string{"name": "platform", "description": "changed outside Terraform"},
		update:        map[string]tftypes.Value{"description": str("Shared platform workspaces")},
		wantSentPatch: map[string]any{"name": "platform", "description": "Shared platform workspaces"},
		wantUpdated:   map[string]string{"name": "platform", "description": "Shared platform workspaces"},
	})
}
