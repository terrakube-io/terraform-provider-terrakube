package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
)

func TestOrganizationVariableDataSource_Schema(t *testing.T) {
	ctx := context.Background()
	d := &OrganizationVariableDataSource{}
	var resp datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", resp.Diagnostics)
	}

	for _, name := range []string{"organization_id", "key"} {
		attr, ok := resp.Schema.Attributes[name].(schema.StringAttribute)
		if !ok || !attr.Required {
			t.Errorf("expected %q to be a required string", name)
		}
	}

	category, ok := resp.Schema.Attributes["category"].(schema.StringAttribute)
	if !ok || !category.Optional || !category.Computed {
		t.Error("expected category to be optional and computed")
	}

	// The value of a sensitive variable is never exported, and the attribute is always sensitive.
	value, ok := resp.Schema.Attributes["value"].(schema.StringAttribute)
	if !ok || !value.Computed || !value.Sensitive {
		t.Error("expected value to be computed and sensitive")
	}
}
