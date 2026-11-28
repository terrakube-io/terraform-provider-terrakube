package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestSelfHostedAgentResource(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	runCrudResourceTests(t, crudResourceCase{
		itemType:   "agent",
		collection: "/api/v1/organization/org-1/agent",
		newSource: func(c *http.Client, endpoint string) resource.Resource {
			return &AgentResource{client: newTestClient(c, endpoint)}
		},
		create: map[string]tftypes.Value{
			"name":        str("build-agent"),
			"description": str("Runs builds"),
			"url":         str("https://agent.example.com"),
		},
		wantSent:          map[string]any{"name": "build-agent", "description": "Runs builds", "url": "https://agent.example.com"},
		wantCreated:       map[string]string{"name": "build-agent", "description": "Runs builds", "url": "https://agent.example.com"},
		drift:             map[string]any{"url": "https://moved.example.com"},
		wantDrifted:       map[string]string{"url": "https://moved.example.com", "name": "build-agent"},
		update:            map[string]tftypes.Value{"url": str("https://agent-2.example.com")},
		wantSentPatch:     map[string]any{"url": "https://agent-2.example.com"},
		deleteMissingIsOK: true,
		wantUpdated:       map[string]string{"url": "https://agent-2.example.com"},
	})
}
