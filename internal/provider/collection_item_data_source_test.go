package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func readCollectionItemDataSource(t *testing.T, key, category string, handler http.Handler) *datasource.ReadResponse {
	t.Helper()
	ctx := context.Background()
	d := &CollectionItemDataSource{}
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

	values := map[string]tftypes.Value{
		"organization_id": tftypes.NewValue(tftypes.String, "org-1"),
		"collection_id":   tftypes.NewValue(tftypes.String, "col-1"),
		"key":             tftypes.NewValue(tftypes.String, key),
	}
	if category != "" {
		values["category"] = tftypes.NewValue(tftypes.String, category)
	}
	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: buildObjectValue(objType, values)}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
	d.Read(ctx, req, resp)
	return resp
}

func collectionItemJSON(id, key, category, value, extra string) string {
	return fmt.Sprintf(`{"type":"item","id":%q,"attributes":{"key":%q,"category":%q,"value":%q,"hcl":false%s}}`,
		id, key, category, value, extra)
}

func TestCollectionItemDataSource_Read(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/organization/org-1/collection/col-1/item" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("filter[item]"); got != `key=="my key";category=="ENV"` {
			t.Errorf("filter = %q", got)
		}
		fmt.Fprint(w, `{"data":[`+
			collectionItemJSON("other", "other key", "ENV", "no", `,"sensitive":false`)+`,`+
			collectionItemJSON("item-1", "my key", "ENV", "hello", `,"sensitive":false,"description":"desc"`)+`]}`)
	})

	resp := readCollectionItemDataSource(t, "my key", "ENV", handler)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	var id, value, description string
	resp.State.GetAttribute(context.Background(), path.Root("id"), &id)
	resp.State.GetAttribute(context.Background(), path.Root("value"), &value)
	resp.State.GetAttribute(context.Background(), path.Root("description"), &description)
	if id != "item-1" || value != "hello" || description != "desc" {
		t.Errorf("unexpected state: id=%s value=%s description=%s", id, value, description)
	}
}

func TestCollectionItemDataSource_SensitiveValueIsNull(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data":[`+collectionItemJSON("item-1", "token", "ENV", "should-not-be-read", `,"sensitive":true`)+`]}`)
	})
	resp := readCollectionItemDataSource(t, "token", "", handler)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	var value, description types.String
	var sensitive types.Bool
	resp.State.GetAttribute(context.Background(), path.Root("value"), &value)
	resp.State.GetAttribute(context.Background(), path.Root("description"), &description)
	resp.State.GetAttribute(context.Background(), path.Root("sensitive"), &sensitive)
	if !sensitive.ValueBool() || !value.IsNull() {
		t.Errorf("sensitive=%v value=%v, want true and null", sensitive, value)
	}
	if !description.IsNull() {
		t.Errorf("description should be null when the item has none, got %v", description)
	}
}

func TestCollectionItemDataSource_Category(t *testing.T) {
	both := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data":[`+
			collectionItemJSON("env-1", "region", "ENV", "a", `,"sensitive":false`)+`,`+
			collectionItemJSON("tf-1", "region", "TERRAFORM", "b", `,"sensitive":false`)+`]}`)
	})

	resp := readCollectionItemDataSource(t, "region", "", both)
	if !resp.Diagnostics.HasError() || resp.Diagnostics.Errors()[0].Summary() != "Multiple collection items found" {
		t.Errorf("expected the ambiguity error, got %v", resp.Diagnostics)
	}

	resp = readCollectionItemDataSource(t, "region", "TERRAFORM", both)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	var id string
	resp.State.GetAttribute(context.Background(), path.Root("id"), &id)
	if id != "tf-1" {
		t.Errorf("id = %q, want tf-1", id)
	}
}

func TestCollectionItemDataSource_ReadErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		summary string
	}{
		{"not found", http.StatusOK, `{"data":[]}`, "Collection item not found"},
		{"other key", http.StatusOK, `{"data":[` + collectionItemJSON("1", "other", "ENV", "", `,"sensitive":false`) + `]}`, "Collection item not found"},
		{"api error", http.StatusForbidden, `{"errors":[{"detail":"forbidden"}]}`, "Error reading collection item: 403 Forbidden"},
		{"invalid payload", http.StatusOK, `not json`, "Unable to unmarshal collection item payload"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			})
			resp := readCollectionItemDataSource(t, "wanted", "", handler)
			if !resp.Diagnostics.HasError() {
				t.Fatal("expected an error diagnostic")
			}
			if got := resp.Diagnostics.Errors()[0].Summary(); got != test.summary {
				t.Errorf("diagnostic = %q, want %q", got, test.summary)
			}
		})
	}
}
