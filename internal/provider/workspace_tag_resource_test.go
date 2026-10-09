package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestWorkspaceTagResourceSchema(t *testing.T) {
	ctx := context.Background()
	r := &WorkspaceTagResource{}
	var response resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", response.Diagnostics)
	}

	for _, name := range []string{"tag_id", "organization_id", "workspace_id"} {
		stringAttribute, ok := response.Schema.Attributes[name].(schema.StringAttribute)
		if !ok || !stringAttribute.Required {
			t.Fatalf("expected %q to be a required StringAttribute", name)
		}
		foundRequiresReplace := false
		for _, modifier := range stringAttribute.PlanModifiers {
			if strings.Contains(modifier.Description(ctx), "destroy and recreate the resource") {
				foundRequiresReplace = true
			}
		}
		if !foundRequiresReplace {
			t.Errorf("expected %q to require replacement when changed", name)
		}
	}

	value, ok := response.Schema.Attributes["value"].(schema.StringAttribute)
	if !ok || !value.Optional || value.Computed || value.Required {
		t.Fatal("expected value to be an optional, non-computed StringAttribute")
	}
	if len(value.PlanModifiers) != 0 {
		t.Error("expected value to be updated in place")
	}
	if len(value.Validators) == 0 {
		t.Error("expected value to validate its length")
	}
}

func TestWorkspaceTagUpdateRequestBody(t *testing.T) {
	tests := map[string]struct {
		value    types.String
		expected any
	}{
		"key/value tag": {value: types.StringValue("dev"), expected: "dev"},
		"key-only tag":  {value: types.StringNull(), expected: nil},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			model := WorkspaceTagResourceModel{
				TagID: types.StringValue("tag-1"),
				Value: test.value,
			}
			body, err := workspaceTagUpdateRequestBody(model, "workspace-tag-1")
			if err != nil {
				t.Fatalf("unexpected marshal error: %v", err)
			}

			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("unmarshalling request body: %v", err)
			}
			data := objectField(t, payload, "data")
			if data["type"] != "workspacetag" || data["id"] != "workspace-tag-1" {
				t.Errorf("unexpected JSON:API resource identifier: %#v", data)
			}
			attributes := objectField(t, data, "attributes")
			actual, exists := attributes["value"]
			if !exists {
				t.Fatalf("expected value to be sent so that it can be cleared, got %s", body)
			}
			if actual != test.expected {
				t.Errorf("expected value %#v, got %#v", test.expected, actual)
			}
		})
	}
}

func TestWorkspaceTagValue(t *testing.T) {
	empty := ""
	dev := "dev"

	if !workspaceTagValue(nil).IsNull() {
		t.Error("expected a null API value to be null in state")
	}
	if !workspaceTagValue(&empty).IsNull() {
		t.Error("expected an empty API value to be null in state")
	}
	if got := workspaceTagValue(&dev); got.ValueString() != "dev" {
		t.Errorf("expected dev, got %s", got)
	}
}
