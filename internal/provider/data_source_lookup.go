package provider

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// findSingleByName returns the one entity in items whose name is exactly name. It adds an
// error and returns nil when there is none, when there is more than one, or when an item has
// an unexpected type. The API filter alone is not enough, since RSQL treats `*` in a value as
// a wildcard, and an empty result would otherwise be returned as a successful read.
//
// where describes the scope for the message, for example " in organization org-1", or is empty.
func findSingleByName[T any](diags *diag.Diagnostics, kind, name, where string, items []interface{}, nameOf func(*T) string) *T {
	var found *T
	for _, item := range items {
		entity, ok := item.(*T)
		if !ok || entity == nil {
			diags.AddError(fmt.Sprintf("Unable to unmarshal %s payload", kind), fmt.Sprintf("Unexpected type in %s payload.", kind))
			return nil
		}
		if nameOf(entity) != name {
			continue
		}
		if found != nil {
			diags.AddError(fmt.Sprintf("Multiple %s entries found", kind),
				fmt.Sprintf("More than one %s named %q was found%s.", kind, name, where))
			return nil
		}
		found = entity
	}
	if found == nil {
		diags.AddError(fmt.Sprintf("%s not found", strings.ToUpper(kind[:1])+kind[1:]),
			fmt.Sprintf("No %s named %q was found%s.", kind, name, where))
	}
	return found
}
