package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestModuleResource_ImportState(t *testing.T) {
	ctx := context.Background()

	r := &ModuleResource{}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics building schema: %v", schemaResp.Diagnostics)
	}
	s := schemaResp.Schema
	objType, ok := s.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("expected schema type to be a tftypes.Object")
	}

	// Read builds /organization/{organization_id}/module/{id}, so both must be set.
	resp := &resource.ImportStateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "org-1,module-1"}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var orgID, moduleID string
	resp.State.GetAttribute(ctx, path.Root("organization_id"), &orgID)
	resp.State.GetAttribute(ctx, path.Root("id"), &moduleID)
	if orgID != "org-1" || moduleID != "module-1" {
		t.Errorf("expected org-1, module-1; got %s, %s", orgID, moduleID)
	}

	for _, id := range []string{"module-1", "org-1,", ",module-1", "org-1,module-1,extra"} {
		invalidResp := &resource.ImportStateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
		r.ImportState(ctx, resource.ImportStateRequest{ID: id}, invalidResp)
		if !invalidResp.Diagnostics.HasError() {
			t.Errorf("expected an error for import ID %q", id)
		}
	}
}
