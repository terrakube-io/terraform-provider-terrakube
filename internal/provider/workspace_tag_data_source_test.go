package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func workspaceTagDataSourceSchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	d := &WorkspaceTagDataSource{}
	var schemaResp datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics building schema: %v", schemaResp.Diagnostics)
	}

	objType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("expected schema type to be a tftypes.Object")
	}

	return schemaResp.Schema, objType
}

func readWorkspaceTagDataSource(t *testing.T, mux *http.ServeMux, name string) *datasource.ReadResponse {
	t.Helper()
	ctx := context.Background()
	s, objType := workspaceTagDataSourceSchemaAndType(t, ctx)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	d := &WorkspaceTagDataSource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	configValue := buildObjectValue(objType, map[string]tftypes.Value{
		"name":            tftypes.NewValue(tftypes.String, name),
		"organization_id": tftypes.NewValue(tftypes.String, "org-1"),
		"workspace_id":    tftypes.NewValue(tftypes.String, "ws-1"),
	})

	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: configValue}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}
	d.Read(ctx, req, resp)
	return resp
}

func workspaceTagTestMux(t *testing.T, name string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization/org-1/tag", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("filter[tag]"); got != "name==\""+name+"\"" {
			t.Errorf("filter[tag] = %q, want %q", got, "name==\""+name+"\"")
		}
		// The server may ignore the filter, so return extra tags to verify exact name matching.
		fmt.Fprint(w, `{"data":[
			{"type":"tag","id":"tag-other","attributes":{"name":"my tag 2"}},
			{"type":"tag","id":"tag-1","attributes":{"name":"my tag"}}
		]}`)
	})
	mux.HandleFunc("/api/v1/organization/org-1/workspace/ws-1/workspaceTag", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[
			{"type":"workspacetag","id":"wstag-other","attributes":{"tagId":"tag-other"}},
			{"type":"workspacetag","id":"wstag-1","attributes":{"tagId":"tag-1","value":"prod"}}
		]}`)
	})
	return mux
}

func TestWorkspaceTagDataSource_Read(t *testing.T) {
	resp := readWorkspaceTagDataSource(t, workspaceTagTestMux(t, "my tag"), "my tag")
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state WorkspaceTagDataSourceModel
	if diags := resp.State.Get(context.Background(), &state); diags.HasError() {
		t.Fatalf("reading state: %v", diags)
	}

	if state.ID.ValueString() != "wstag-1" {
		t.Errorf("ID = %q, want wstag-1", state.ID.ValueString())
	}
	if state.TagID.ValueString() != "tag-1" {
		t.Errorf("TagID = %q, want tag-1", state.TagID.ValueString())
	}
	if state.Value.ValueString() != "prod" {
		t.Errorf("Value = %q, want prod", state.Value.ValueString())
	}
}

func TestWorkspaceTagDataSource_TagNotFound(t *testing.T) {
	resp := readWorkspaceTagDataSource(t, workspaceTagTestMux(t, "missing"), "missing")
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected error for unknown tag")
	}
}

func TestWorkspaceTagDataSource_NotAttached(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization/org-1/tag", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"type":"tag","id":"tag-1","attributes":{"name":"my tag"}}]}`)
	})
	mux.HandleFunc("/api/v1/organization/org-1/workspace/ws-1/workspaceTag", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[]}`)
	})

	resp := readWorkspaceTagDataSource(t, mux, "my tag")
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected error when tag is not attached to the workspace")
	}
	if !strings.Contains(resp.Diagnostics.Errors()[0].Summary(), "Workspace Tag Not Found") {
		t.Errorf("unexpected error: %v", resp.Diagnostics)
	}
}

func TestWorkspaceTagDataSource_HTTPError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization/org-1/tag", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"errors":[{"detail":"forbidden"}]}`)
	})

	resp := readWorkspaceTagDataSource(t, mux, "my tag")
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected error for non-200 response")
	}
}

// A key-only tag reads as null whether the API leaves value out (Terrakube versions before key/value tags),
// returns null, or returns "", matching the terrakube_workspace_tag resource.
func TestWorkspaceTagDataSource_ReadKeyOnly(t *testing.T) {
	cases := map[string]string{
		"value omitted": `{"tagId":"tag-1"}`,
		"value null":    `{"tagId":"tag-1","value":null}`,
		"value empty":   `{"tagId":"tag-1","value":""}`,
	}

	for name, attributes := range cases {
		t.Run(name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v1/organization/org-1/tag", func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"data":[{"type":"tag","id":"tag-1","attributes":{"name":"my tag"}}]}`)
			})
			mux.HandleFunc("/api/v1/organization/org-1/workspace/ws-1/workspaceTag", func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"data":[{"type":"workspacetag","id":"wstag-1","attributes":%s}]}`, attributes)
			})

			resp := readWorkspaceTagDataSource(t, mux, "my tag")
			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
			}

			var state WorkspaceTagDataSourceModel
			if diags := resp.State.Get(context.Background(), &state); diags.HasError() {
				t.Fatalf("reading state: %v", diags)
			}

			if state.ID.ValueString() != "wstag-1" {
				t.Errorf("ID = %q, want wstag-1", state.ID.ValueString())
			}
			if !state.Value.IsNull() {
				t.Errorf("Value = %q, want null", state.Value.ValueString())
			}
		})
	}
}
