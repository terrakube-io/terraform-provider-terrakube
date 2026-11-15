package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestOrganizationVariableResource(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	flag := func(v bool) tftypes.Value { return tftypes.NewValue(tftypes.Bool, v) }
	runCrudResourceTests(t, crudResourceCase{
		itemType:   "globalvar",
		collection: "/api/v1/organization/org-1/globalvar",
		newSource: func(c *http.Client, endpoint string) resource.Resource {
			return &OrganizationVariableResource{client: c, endpoint: endpoint, token: "test-token"}
		},
		create: map[string]tftypes.Value{
			"key":         str("AWS_REGION"),
			"value":       str("eu-west-1"),
			"description": str("Default region"),
			"category":    str("ENV"),
			"sensitive":   flag(false),
			"hcl":         flag(false),
		},
		wantSent: map[string]any{
			"key": "AWS_REGION", "value": "eu-west-1", "description": "Default region",
			"category": "ENV", "sensitive": false, "hcl": false,
		},
		wantCreated:       map[string]string{"key": "AWS_REGION", "value": "eu-west-1", "category": "ENV", "sensitive": "false", "hcl": "false"},
		drift:             map[string]any{"value": "us-east-1"},
		wantDrifted:       map[string]string{"value": "us-east-1", "key": "AWS_REGION"},
		update:            map[string]tftypes.Value{"value": str("eu-central-1"), "description": str("Region for the EU stack")},
		wantSentPatch:     map[string]any{"value": "eu-central-1", "description": "Region for the EU stack"},
		deleteMissingIsOK: true,
		wantUpdated:       map[string]string{"value": "eu-central-1", "description": "Region for the EU stack"},
	})
}
