package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestWorkspaceScheduleResource_ImportState(t *testing.T) {
	ctx := context.Background()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/workspace/ws-1/schedule/sch-1", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			t.Errorf("unexpected method %s", req.Method)
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		_, _ = w.Write([]byte(`{"data":{"type":"schedule","id":"sch-1","attributes":{` +
			`"cron":"0 0 2 * * ?","templateReference":"tpl-1"}}}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	r := &WorkspaceScheduleResource{client: server.Client(), endpoint: server.URL, token: "test-token"}

	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	objType, ok := s.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("expected schema type to be a tftypes.Object")
	}
	newState := func() tfsdk.State {
		return tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}
	}

	importResp := &resource.ImportStateResponse{State: newState()}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "ws-1,sch-1"}, importResp)
	if importResp.Diagnostics.HasError() {
		t.Fatalf("unexpected import diagnostics: %v", importResp.Diagnostics)
	}

	// Read must be able to refresh the imported state.
	readResp := &resource.ReadResponse{State: importResp.State}
	r.Read(ctx, resource.ReadRequest{State: importResp.State}, readResp)
	if readResp.Diagnostics.HasError() {
		t.Fatalf("unexpected read diagnostics: %v", readResp.Diagnostics)
	}

	want := map[string]string{
		"id":           "sch-1",
		"workspace_id": "ws-1",
		"template_id":  "tpl-1",
		"schedule":     "0 0 2 * * ?",
	}
	for attr, expected := range want {
		var got string
		readResp.State.GetAttribute(ctx, path.Root(attr), &got)
		if got != expected {
			t.Errorf("%s: expected %q, got %q", attr, expected, got)
		}
	}

	for _, id := range []string{"sch-1", "ws-1,", ",sch-1", "org-1,ws-1,sch-1"} {
		invalidResp := &resource.ImportStateResponse{State: newState()}
		r.ImportState(ctx, resource.ImportStateRequest{ID: id}, invalidResp)
		if !invalidResp.Diagnostics.HasError() {
			t.Errorf("expected an error for import ID %q", id)
		}
	}
}
