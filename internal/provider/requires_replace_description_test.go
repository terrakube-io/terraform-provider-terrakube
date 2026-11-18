package provider

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// forcesNewResource is the note an attribute's description needs when changing it replaces the
// resource, so users read it in the docs instead of finding out from the plan.
const forcesNewResource = "forces a new resource"

// TestResourceSchemas_RequiresReplaceIsDocumented fails when an attribute with a RequiresReplace
// plan modifier doesn't say so in its description.
func TestResourceSchemas_RequiresReplaceIsDocumented(t *testing.T) {
	ctx := context.Background()
	found := 0
	for _, f := range New("test")().Resources(ctx) {
		r := f()
		var meta resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "terrakube"}, &meta)
		var resp resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &resp)

		var missing []string
		walkResourceAttributes("", resp.Schema.Attributes, resp.Schema.Blocks, func(name string, a schema.Attribute) {
			if !requiresReplace(ctx, a) {
				return
			}
			found++
			for _, d := range []string{a.GetDescription(), a.GetMarkdownDescription()} {
				if d != "" && !strings.Contains(d, forcesNewResource) {
					missing = append(missing, name)
					return
				}
			}
			if a.GetDescription() == "" && a.GetMarkdownDescription() == "" {
				missing = append(missing, name)
			}
		})
		sort.Strings(missing)
		for _, name := range missing {
			t.Errorf("%s.%s replaces the resource when it changes, but its description doesn't say %q", meta.TypeName, name, forcesNewResource)
		}
	}
	if found == 0 {
		t.Fatal("no attribute with a RequiresReplace plan modifier was found, so the test checks nothing")
	}
}

// walkResourceAttributes calls fn for every attribute, including those nested in attributes and
// blocks. Nested names are joined with dots.
func walkResourceAttributes(prefix string, attrs map[string]schema.Attribute, blocks map[string]schema.Block, fn func(string, schema.Attribute)) {
	for name, a := range attrs {
		fn(prefix+name, a)
		if nested, ok := a.(interface {
			GetNestedObject() schema.NestedAttributeObject
		}); ok {
			obj := nested.GetNestedObject()
			walkResourceAttributes(prefix+name+".", obj.Attributes, nil, fn)
		}
	}
	for name, b := range blocks {
		obj := b.GetNestedObject()
		if nested, ok := obj.(schema.NestedBlockObject); ok {
			walkResourceAttributes(prefix+name+".", nested.Attributes, nested.Blocks, fn)
		}
	}
}

// requiresReplace reports whether an attribute has one of the framework's RequiresReplace plan
// modifiers (RequiresReplace, RequiresReplaceIf, RequiresReplaceIfConfigured). They are
// unexported types, so they are recognised by name.
func requiresReplace(_ context.Context, a schema.Attribute) bool {
	modifiers := reflect.ValueOf(a).FieldByName("PlanModifiers")
	if !modifiers.IsValid() {
		return false
	}
	for i := 0; i < modifiers.Len(); i++ {
		m := modifiers.Index(i).Elem()
		if strings.HasPrefix(reflect.Indirect(m).Type().Name(), "requiresReplace") {
			return true
		}
	}
	return false
}
