package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"terraform-provider-terrakube/internal/client"
)

// An output whose value can't be typed or converted is an error, not a silently wrong output.
func TestConvertWorkspaceOutputs_Errors(t *testing.T) {
	tests := map[string]any{
		// inferAttrType has no type for a Go struct.
		"unsupported type": struct{}{},
		// inferAttrType types an int as a number, but convertToAttrValue only accepts the float64
		// that encoding/json produces.
		"conversion fails": 42,
	}
	for name, value := range tests {
		t.Run(name, func(t *testing.T) {
			var diags diag.Diagnostics
			_, found := convertWorkspaceOutputs(map[string]any{"bad": map[string]any{"value": value, "sensitive": false}}, &diags)
			if found {
				t.Error("found should be false on an error")
			}
			if !diags.HasError() || diags.Errors()[0].Summary() != "Unable to read workspace output" {
				t.Fatalf("expected an output error, got %v", diags)
			}
		})
	}
}

func TestConvertWorkspaceOutputs_NotAnObject(t *testing.T) {
	var diags diag.Diagnostics
	if _, found := convertWorkspaceOutputs(map[string]any{"region": "eu-west-1"}, &diags); found || !diags.HasError() {
		t.Fatalf("expected an error, got found=%t diags=%v", found, diags)
	}
}

// An unexpected entity type from the API is reported instead of dereferencing nil.
func TestJSONAPIEntityAt(t *testing.T) {
	var diags diag.Diagnostics
	if got := jsonapiEntityAt[client.OrganizationEntity]([]any{&client.OrganizationEntity{ID: "org-1"}}, 0, "organization", &diags); got == nil || got.ID != "org-1" || diags.HasError() {
		t.Fatalf("expected the organization, got %v %v", got, diags)
	}

	for name, item := range map[string]any{
		"wrong type": &client.WorkspaceEntity{ID: "ws-1"},
		"nil":        (*client.OrganizationEntity)(nil),
		"not a ptr":  "org-1",
	} {
		t.Run(name, func(t *testing.T) {
			var diags diag.Diagnostics
			if got := jsonapiEntityAt[client.OrganizationEntity]([]any{item}, 0, "organization", &diags); got != nil {
				t.Errorf("expected nil, got %v", got)
			}
			if !diags.HasError() || diags.Errors()[0].Summary() != "Unexpected organization response" {
				t.Errorf("expected an error, got %v", diags)
			}
		})
	}
}
