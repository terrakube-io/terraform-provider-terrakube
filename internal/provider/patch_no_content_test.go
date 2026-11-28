package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Terrakube answers a PATCH with 204 and no body, and terrakube-go then reads the item back. A
// failed read-back does not undo the PATCH, so it must not fail the update or the organization's
// soft delete.
func TestPatchNoContent(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	cases := []struct {
		name   string
		tc     crudResourceCase
		update string // the attribute the update changes
	}{
		{"organization", crudResourceCase{
			itemType: "organization", collection: "/api/v1/organization", noOrganization: true,
			newSource: func(c *http.Client, e string) resource.Resource {
				return &OrganizationResource{client: newTestClient(c, e)}
			},
			create: map[string]tftypes.Value{"name": str("platform"), "execution_mode": str("remote")},
			update: map[string]tftypes.Value{"description": str("changed")},
		}, "description"},
		{"organization_tag", crudResourceCase{
			itemType: "tag", collection: "/api/v1/organization/org-1/tag",
			newSource: func(c *http.Client, e string) resource.Resource {
				return &OrganizationTagResource{client: newTestClient(c, e)}
			},
			create: map[string]tftypes.Value{"name": str("production")},
			update: map[string]tftypes.Value{"name": str("changed")},
		}, "name"},
		{"organization_template", crudResourceCase{
			itemType: "template", collection: "/api/v1/organization/org-1/template",
			newSource: func(c *http.Client, e string) resource.Resource {
				return &OrganizationTemplateResource{client: newTestClient(c, e)}
			},
			create: map[string]tftypes.Value{"name": str("plan"), "content": str("flow: []\n")},
			update: map[string]tftypes.Value{"name": str("changed")},
		}, "name"},
		{"collection", crudResourceCase{
			itemType: "collection", collection: "/api/v1/organization/org-1/collection",
			newSource: func(c *http.Client, e string) resource.Resource {
				return &CollectionResource{client: newTestClient(c, e)}
			},
			create: map[string]tftypes.Value{"name": str("shared-settings"), "priority": tftypes.NewValue(tftypes.Number, 10)},
			update: map[string]tftypes.Value{"name": str("changed")},
		}, "name"},
		{"collection_item", crudResourceCase{
			itemType: "item", collection: "/api/v1/organization/org-1/collection/col-1/item",
			scope: map[string]tftypes.Value{"collection_id": str("col-1")},
			newSource: func(c *http.Client, e string) resource.Resource {
				return &CollectionItemResource{client: newTestClient(c, e)}
			},
			create: map[string]tftypes.Value{
				"key": str("REGION"), "value": str("eu-west-1"), "category": str("ENV"),
				"sensitive": tftypes.NewValue(tftypes.Bool, false), "hcl": tftypes.NewValue(tftypes.Bool, false),
			},
			update: map[string]tftypes.Value{"key": str("changed")},
		}, "key"},
		{"organization_variable", crudResourceCase{
			itemType: "globalvar", collection: "/api/v1/organization/org-1/globalvar",
			newSource: func(c *http.Client, e string) resource.Resource {
				return &OrganizationVariableResource{client: newTestClient(c, e)}
			},
			create: map[string]tftypes.Value{
				"key": str("AWS_REGION"), "value": str("eu-west-1"), "description": str("Default region"), "category": str("ENV"),
				"sensitive": tftypes.NewValue(tftypes.Bool, false), "hcl": tftypes.NewValue(tftypes.Bool, false),
			},
			update: map[string]tftypes.Value{"key": str("changed")},
		}, "key"},
		{"workspace_variable", crudResourceCase{
			itemType: "variable", collection: "/api/v1/organization/org-1/workspace/ws-1/variable",
			scope: map[string]tftypes.Value{"workspace_id": str("ws-1")},
			newSource: func(c *http.Client, e string) resource.Resource {
				return &WorkspaceVariableResource{client: newTestClient(c, e)}
			},
			create: map[string]tftypes.Value{
				"key": str("AWS_REGION"), "value": str("eu-west-1"), "description": str("Default region"), "category": str("ENV"),
				"sensitive": tftypes.NewValue(tftypes.Bool, false), "hcl": tftypes.NewValue(tftypes.Bool, false),
			},
			update: map[string]tftypes.Value{"key": str("changed")},
		}, "key"},
		{"project", crudResourceCase{
			itemType: "project", collection: "/api/v1/organization/org-1/project",
			newSource: func(c *http.Client, e string) resource.Resource { return &ProjectResource{client: newTestClient(c, e)} },
			create:    map[string]tftypes.Value{"name": str("platform")},
			update:    map[string]tftypes.Value{"name": str("changed")},
		}, "name"},
	}

	// These resources use the item UpdateFields read back instead of reading it again.
	readsBackOnce := map[string]bool{"organization": true, "organization_tag": true, "organization_template": true, "project": true}

	for _, c := range cases {
		t.Run(c.name+"/update", func(t *testing.T) {
			h := newCrudHarness(t, c.tc)
			prior := h.createdState()
			h.server.patchNoContent = true
			resp := h.update(prior)
			if resp.Diagnostics.HasError() {
				t.Fatalf("Update returned diagnostics: %v", resp.Diagnostics)
			}
			h.checkState(resp.State, map[string]string{c.update: "changed"})
			if readsBackOnce[c.name] {
				checkOneReadAfterPatch(t, h.server)
			}
		})

		t.Run(c.name+"/update survives a failed read-back", func(t *testing.T) {
			h := newCrudHarness(t, c.tc)
			prior := h.createdState()
			h.server.patchNoContent = true
			h.server.fail[http.MethodGet] = http.StatusBadGateway // only the read-back inside UpdateFields
			resp := h.update(prior)
			if resp.Diagnostics.HasError() {
				t.Fatalf("Update returned diagnostics: %v", resp.Diagnostics)
			}
			h.checkState(resp.State, map[string]string{c.update: "changed"})
		})
	}

	t.Run("organization/delete survives a failed read-back", func(t *testing.T) {
		h := newCrudHarness(t, cases[0].tc)
		prior := h.createdState()
		h.server.patchNoContent = true
		h.server.fail[http.MethodGet] = http.StatusBadGateway
		if resp := h.delete(prior); resp.Diagnostics.HasError() {
			t.Fatalf("Delete returned diagnostics: %v", resp.Diagnostics)
		}
		request, _ := h.server.lastRequest(http.MethodPatch)
		checkSent(t, request, map[string]any{"disabled": true})
	})
}

// checkOneReadAfterPatch fails the test unless exactly one GET followed the PATCH.
func checkOneReadAfterPatch(t *testing.T, s *crudServer) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	patched, reads := false, 0
	for _, r := range s.requests {
		switch {
		case r.method == http.MethodPatch:
			patched = true
		case patched && r.method == http.MethodGet:
			reads++
		}
	}
	if reads != 1 {
		t.Errorf("expected one GET after the PATCH, got %d", reads)
	}
}
