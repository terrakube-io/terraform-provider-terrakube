package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// patchAttributes collects the JSON:API attributes of a sparse write
// (terrakube.Patch.Attributes). A null value is sent as null, which clears the
// attribute on the server; an unknown value is left out, so the server keeps
// or computes it.
type patchAttributes map[string]any

func (a patchAttributes) setString(name string, value types.String) {
	switch {
	case value.IsUnknown():
	case value.IsNull():
		a[name] = nil
	default:
		a[name] = value.ValueString()
	}
}

func (a patchAttributes) setBool(name string, value types.Bool) {
	switch {
	case value.IsUnknown():
	case value.IsNull():
		a[name] = nil
	default:
		a[name] = value.ValueBool()
	}
}

func (a patchAttributes) setInt64(name string, value types.Int64) {
	switch {
	case value.IsUnknown():
	case value.IsNull():
		a[name] = nil
	default:
		a[name] = value.ValueInt64()
	}
}
