package provider

import (
	"context"
	"encoding/json"
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

	r := &CollectionReferenceResource{client: newTestClient(server.Client(), server.URL)}

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

// collectionReferenceServer creates a reference under the collection and serves it at the flat
// /api/v1/reference/{id} path, as Terrakube does. It records the body of every POST and PATCH.
// A PATCH answers 204; failReadBack makes the next GET fail with 502.
type collectionReferenceServer struct {
	bodies       map[string]map[string]any
	failReadBack bool
}

func (s *collectionReferenceServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost || r.Method == http.MethodPatch {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.bodies[r.Method+" "+r.URL.Path] = body
	}
	reference := `{"data":{"type":"reference","id":"ref-1","attributes":{"description":"d"},` +
		`"relationships":{"workspace":{"data":{"type":"workspace","id":"ws-1"}},"collection":{"data":{"type":"collection","id":"col-1"}}}}}`
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/organization/org-1/collection/col-1/reference":
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, reference)
	case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/reference/ref-1":
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/reference/ref-1":
		if s.failReadBack {
			s.failReadBack = false
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, reference)
	default:
		w.WriteHeader(http.StatusTeapot)
	}
}

// The reference links the workspace and the collection with JSON:API relationships, as the
// hand-written client did, on create and on update.
func TestCollectionReferenceResource_SendsRelationships(t *testing.T) {
	ctx := context.Background()
	s, objType := collectionReferenceSchemaAndType(t, ctx)

	srv := &collectionReferenceServer{bodies: map[string]map[string]any{}}
	server := httptest.NewServer(srv)
	defer server.Close()
	r := &CollectionReferenceResource{client: newTestClient(server.Client(), server.URL)}

	values := func(id tftypes.Value) tftypes.Value {
		return buildObjectValue(objType, map[string]tftypes.Value{
			"id":              id,
			"organization_id": tftypes.NewValue(tftypes.String, "org-1"),
			"workspace_id":    tftypes.NewValue(tftypes.String, "ws-1"),
			"collection_id":   tftypes.NewValue(tftypes.String, "col-1"),
			"description":     tftypes.NewValue(tftypes.String, "d"),
		})
	}
	checkRelationships := func(t *testing.T, key string) {
		t.Helper()
		body, ok := srv.bodies[key]
		if !ok {
			t.Fatalf("no %s request was sent", key)
		}
		data, _ := body["data"].(map[string]any)
		if data["type"] != "reference" {
			t.Errorf("%s type = %v, want reference", key, data["type"])
		}
		relationships, _ := data["relationships"].(map[string]any)
		for name, id := range map[string]string{"workspace": "ws-1", "collection": "col-1"} {
			link, _ := relationships[name].(map[string]any)
			target, _ := link["data"].(map[string]any)
			if target["type"] != name || target["id"] != id {
				t.Errorf("%s relationship %s = %v, want type %s id %s", key, name, link, name, id)
			}
		}
	}

	plan := values(tftypes.NewValue(tftypes.String, tftypes.UnknownValue))
	createResp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: plan}, Config: tfsdk.Config{Schema: s, Raw: plan}}, createResp)
	if createResp.Diagnostics.HasError() {
		t.Fatalf("Create returned diagnostics: %v", createResp.Diagnostics)
	}
	checkRelationships(t, "POST /api/v1/organization/org-1/collection/col-1/reference")

	// The PATCH answers 204 and the read-back inside UpdateFields fails: the update still succeeds.
	srv.failReadBack = true
	known := values(tftypes.NewValue(tftypes.String, "ref-1"))
	updateResp := &resource.UpdateResponse{State: createResp.State}
	r.Update(ctx, resource.UpdateRequest{
		State:  createResp.State,
		Plan:   tfsdk.Plan{Schema: s, Raw: known},
		Config: tfsdk.Config{Schema: s, Raw: known},
	}, updateResp)
	if updateResp.Diagnostics.HasError() {
		t.Fatalf("Update returned diagnostics: %v", updateResp.Diagnostics)
	}
	checkRelationships(t, "PATCH /api/v1/reference/ref-1")

	var result CollectionReferenceResourceModel
	if diags := updateResp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}
	if result.ID.ValueString() != "ref-1" || result.WorkspaceId.ValueString() != "ws-1" || result.CollectionId.ValueString() != "col-1" {
		t.Errorf("unexpected state after update: %+v", result)
	}
}
