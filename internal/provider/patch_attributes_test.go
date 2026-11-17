package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func assertAttributes(t *testing.T, got patchAttributes, want map[string]any) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("attributes = %#v, want %#v", map[string]any(got), want)
	}
	for key, value := range want {
		v, ok := got[key]
		if !ok || v != value {
			t.Errorf("attributes[%q] = %#v (present %v), want %#v", key, v, ok, value)
		}
	}
}

func TestPatchAttributesCreateLeavesOutNull(t *testing.T) {
	attrs := patchAttributes{}
	attrs.createString("name", types.StringValue("platform"))
	attrs.createString("description", types.StringNull())
	attrs.createString("computed", types.StringUnknown())
	attrs.createBool("disabled", types.BoolValue(false))
	attrs.createBool("nullBool", types.BoolNull())
	attrs.createBool("unknownBool", types.BoolUnknown())
	attrs.createInt64("priority", types.Int64Value(0))
	attrs.createInt64("nullInt", types.Int64Null())
	attrs.createInt64("unknownInt", types.Int64Unknown())

	assertAttributes(t, attrs, map[string]any{
		"name":     "platform",
		"disabled": false,
		"priority": int64(0),
	})
}

func TestPatchAttributesUpdateSendsNullOnlyToClear(t *testing.T) {
	attrs := patchAttributes{}
	// Removed from the configuration: the prior value is cleared.
	attrs.updateString("description", types.StringNull(), types.StringValue("old"))
	attrs.updateBool("disabled", types.BoolNull(), types.BoolValue(true))
	attrs.updateInt64("priority", types.Int64Null(), types.Int64Value(5))
	// Null before and after: never sent, so an older server is not asked about it.
	attrs.updateString("newerAttribute", types.StringNull(), types.StringNull())
	attrs.updateBool("newerBool", types.BoolNull(), types.BoolNull())
	attrs.updateInt64("newerInt", types.Int64Null(), types.Int64Null())
	// Set or changed: sent as is.
	attrs.updateString("name", types.StringValue("platform"), types.StringValue("old"))
	attrs.updateBool("enabled", types.BoolValue(false), types.BoolNull())
	attrs.updateInt64("limit", types.Int64Value(0), types.Int64Null())
	// Unknown: left out whatever the prior state.
	attrs.updateString("computed", types.StringUnknown(), types.StringValue("old"))
	attrs.updateBool("unknownBool", types.BoolUnknown(), types.BoolValue(true))
	attrs.updateInt64("unknownInt", types.Int64Unknown(), types.Int64Value(1))
	// An unknown prior (never set by Terraform) is treated like null.
	attrs.updateString("unknownPrior", types.StringNull(), types.StringUnknown())

	assertAttributes(t, attrs, map[string]any{
		"description": nil,
		"disabled":    nil,
		"priority":    nil,
		"name":        "platform",
		"enabled":     false,
		"limit":       int64(0),
	})
}
