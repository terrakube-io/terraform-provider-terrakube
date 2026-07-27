package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func collectionReferenceSchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	r := &CollectionReferenceResource{}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics building schema: %v", schemaResp.Diagnostics)
	}

	objType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("expected schema type to be a tftypes.Object")
	}

	return schemaResp.Schema, objType
}

// TestCollectionReferenceResource_Read_HandlesNullWorkspaceRelationship covers
// the drift scenario where Terrakube returns relationships.workspace.data = null
// (e.g. the referenced workspace was deleted out of band). Reading must not
// panic and should keep the last-known workspace_id instead of crashing.
func TestCollectionReferenceResource_Read_HandlesNullWorkspaceRelationship(t *testing.T) {
	ctx := context.Background()
	s, objType := collectionReferenceSchemaAndType(t, ctx)

	const (
		refID   = "ref-1"
		orgID   = "org-1"
		oldWsID = "ws-old"
		colID   = "col-1"
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/reference/"+refID, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"data":{"type":"reference","id":%q,"attributes":{"description":"d"},`+
			`"relationships":{"workspace":{"data":null},"collection":{"data":{"type":"collection","id":%q}}}}}`,
			refID, colID)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &CollectionReferenceResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	stateValue := buildObjectValue(objType, map[string]tftypes.Value{
		"id":              tftypes.NewValue(tftypes.String, refID),
		"organization_id": tftypes.NewValue(tftypes.String, orgID),
		"workspace_id":    tftypes.NewValue(tftypes.String, oldWsID),
		"collection_id":   tftypes.NewValue(tftypes.String, colID),
		"description":     tftypes.NewValue(tftypes.String, "d"),
	})

	req := resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: stateValue}}
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: s}}

	r.Read(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics)
	}

	var result CollectionReferenceResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}

	if result.WorkspaceId.ValueString() != oldWsID {
		t.Errorf("expected workspace_id to keep last-known value %q when relationship data is null, got %q", oldWsID, result.WorkspaceId.ValueString())
	}
	if result.CollectionId.ValueString() != colID {
		t.Errorf("expected collection_id to be updated to %q, got %q", colID, result.CollectionId.ValueString())
	}
}
