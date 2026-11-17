package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// patchAttributes collects the JSON:API attributes of a sparse write
// (terrakube.Patch.Attributes). An unknown value is always left out, so the
// server keeps or computes it.
//
// Elide rejects an attribute it does not know, even as null, so null is never
// sent for an attribute the server may not have:
//   - create* methods leave a null value out.
//   - update* methods send null only when the prior state had a value, which
//     clears it on the server; a value that was and stays null is left out.
type patchAttributes map[string]any

func (a patchAttributes) createString(name string, value types.String) {
	a.updateString(name, value, types.StringNull())
}

func (a patchAttributes) updateString(name string, value, prior types.String) {
	switch {
	case value.IsUnknown():
	case value.IsNull():
		if !prior.IsNull() && !prior.IsUnknown() {
			a[name] = nil
		}
	default:
		a[name] = value.ValueString()
	}
}

func (a patchAttributes) createBool(name string, value types.Bool) {
	a.updateBool(name, value, types.BoolNull())
}

func (a patchAttributes) updateBool(name string, value, prior types.Bool) {
	switch {
	case value.IsUnknown():
	case value.IsNull():
		if !prior.IsNull() && !prior.IsUnknown() {
			a[name] = nil
		}
	default:
		a[name] = value.ValueBool()
	}
}

func (a patchAttributes) createInt64(name string, value types.Int64) {
	a.updateInt64(name, value, types.Int64Null())
}

func (a patchAttributes) updateInt64(name string, value, prior types.Int64) {
	switch {
	case value.IsUnknown():
	case value.IsNull():
		if !prior.IsNull() && !prior.IsUnknown() {
			a[name] = nil
		}
	default:
		a[name] = value.ValueInt64()
	}
}
