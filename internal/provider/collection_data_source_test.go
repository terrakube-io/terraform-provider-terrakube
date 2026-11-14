package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func readCollectionDataSource(t *testing.T, name string, handler http.Handler) *datasource.ReadResponse {
	t.Helper()
	ctx := context.Background()
	d := &CollectionDataSource{}
	var schemaResp datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics building schema: %v", schemaResp.Diagnostics)
	}
	objType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("expected schema type to be a tftypes.Object")
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	d.client = server.Client()
	d.endpoint = server.URL
	d.token = "test-token"

	configValue := buildObjectValue(objType, map[string]tftypes.Value{
		"name":            tftypes.NewValue(tftypes.String, name),
		"organization_id": tftypes.NewValue(tftypes.String, "org-1"),
	})
	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: configValue}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
	d.Read(ctx, req, resp)
	return resp
}

func TestCollectionDataSource_Read(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/organization/org-1/collection" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("filter[collection]"); got != `name=="shared & more"` {
			t.Errorf("filter = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		fmt.Fprint(w, `{"data":[
			{"type":"collection","id":"other","attributes":{"name":"other","description":"wrong","priority":1}},
			{"type":"collection","id":"col-1","attributes":{"name":"shared & more","description":"Shared settings","priority":20}}
		]}`)
	})

	resp := readCollectionDataSource(t, "shared & more", handler)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	var state CollectionDataSourceModel
	if diags := resp.State.Get(context.Background(), &state); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}
	if state.ID.ValueString() != "col-1" || state.OrganizationId.ValueString() != "org-1" ||
		state.Name.ValueString() != "shared & more" || state.Description.ValueString() != "Shared settings" ||
		state.Priority.ValueInt32() != 20 {
		t.Errorf("unexpected state: %+v", state)
	}
}

func TestCollectionDataSource_ReadWithoutDescription(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data":[{"type":"collection","id":"col-1","attributes":{"name":"shared","priority":1}}]}`)
	})
	resp := readCollectionDataSource(t, "shared", handler)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	var state CollectionDataSourceModel
	if diags := resp.State.Get(context.Background(), &state); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}
	if !state.Description.IsNull() {
		t.Errorf("description should be null, got %q", state.Description.ValueString())
	}
}

func TestCollectionDataSource_ReadErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		summary string
	}{
		{"not found", http.StatusOK, `{"data":[]}`, "Collection not found"},
		{"unmatched", http.StatusOK, `{"data":[{"type":"collection","id":"1","attributes":{"name":"other"}}]}`, "Collection not found"},
		{"duplicate", http.StatusOK, `{"data":[{"type":"collection","id":"1","attributes":{"name":"shared"}},{"type":"collection","id":"2","attributes":{"name":"shared"}}]}`, "Multiple collections found"},
		{"api error", http.StatusForbidden, `{"errors":[{"detail":"forbidden"}]}`, "Error reading collection: 403 Forbidden"},
		{"invalid payload", http.StatusOK, `not json`, "Unable to unmarshal collection payload"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			})
			resp := readCollectionDataSource(t, "shared", handler)
			if !resp.Diagnostics.HasError() {
				t.Fatal("expected an error diagnostic")
			}
			if got := resp.Diagnostics.Errors()[0].Summary(); got != test.summary {
				t.Errorf("diagnostic = %q, want %q", got, test.summary)
			}
		})
	}
}
