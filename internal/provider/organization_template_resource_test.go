package provider

import (
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Terrakube stores the template flow base64 encoded, and the resource sends and reads it that way.
func TestOrganizationTemplateResource(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	encode := func(v string) string { return base64.StdEncoding.EncodeToString([]byte(v)) }

	const flow = "flow:\n  - type: terraformPlan\n    step: 100\n"
	const changedFlow = "flow:\n  - type: terraformApply\n    step: 100\n"
	const updatedFlow = "flow:\n  - type: terraformPlan\n    step: 100\n  - type: terraformApply\n    step: 200\n"

	runCrudResourceTests(t, crudResourceCase{
		itemType:   "template",
		collection: "/template",
		newSource: func(c *http.Client, endpoint string) resource.Resource {
			return &OrganizationTemplateResource{client: c, endpoint: endpoint, token: "test-token"}
		},
		create: map[string]tftypes.Value{
			"name":        str("plan-and-apply"),
			"description": str("Plan, then apply"),
			"version":     str("1.0.0"),
			"content":     str(flow),
		},
		wantSent: map[string]any{
			"name": "plan-and-apply", "description": "Plan, then apply", "version": "1.0.0", "tcl": encode(flow),
		},
		wantCreated: map[string]string{"name": "plan-and-apply", "version": "1.0.0", "content": flow},
		drift:       map[string]any{"tcl": encode(changedFlow), "version": "1.0.1"},
		wantDrifted: map[string]string{"content": changedFlow, "version": "1.0.1"},
		update: map[string]tftypes.Value{
			"version": str("2.0.0"),
			"content": str(updatedFlow),
		},
		wantSentPatch: map[string]any{"version": "2.0.0", "tcl": encode(updatedFlow)},
		wantUpdated:   map[string]string{"version": "2.0.0", "content": updatedFlow},
	})
}
