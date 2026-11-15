package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestPatchAttributes(t *testing.T) {
	attrs := patchAttributes{}
	attrs.setString("name", types.StringValue("platform"))
	attrs.setString("description", types.StringNull())
	attrs.setString("computed", types.StringUnknown())
	attrs.setBool("disabled", types.BoolValue(false))
	attrs.setBool("unknownBool", types.BoolUnknown())
	attrs.setBool("nullBool", types.BoolNull())
	attrs.setInt64("priority", types.Int64Value(0))
	attrs.setInt64("unknownInt", types.Int64Unknown())
	attrs.setInt64("nullInt", types.Int64Null())

	want := map[string]any{
		"name":        "platform",
		"description": nil,
		"disabled":    false,
		"nullBool":    nil,
		"priority":    int64(0),
		"nullInt":     nil,
	}
	if len(attrs) != len(want) {
		t.Fatalf("attributes = %#v, want %#v", map[string]any(attrs), want)
	}
	for key, value := range want {
		got, ok := attrs[key]
		if !ok || got != value {
			t.Errorf("attributes[%q] = %#v (present %v), want %#v", key, got, ok, value)
		}
	}
}
