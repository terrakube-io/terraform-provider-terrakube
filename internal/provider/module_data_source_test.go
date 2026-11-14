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

func readModuleDataSource(t *testing.T, name, providerName string, handler http.Handler) *datasource.ReadResponse {
	t.Helper()
	ctx := context.Background()
	d := &ModuleDataSource{}
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
		"provider_name":   tftypes.NewValue(tftypes.String, providerName),
		"organization_id": tftypes.NewValue(tftypes.String, "org-1"),
	})
	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: configValue}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
	d.Read(ctx, req, resp)
	return resp
}

func TestModuleDataSource_Read(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/organization/org-1/module" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("filter[module]"); got != `name=="vpc & more";provider=="aws"` {
			t.Errorf("filter = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		fmt.Fprint(w, `{"data":[
			{"type":"module","id":"other","attributes":{"name":"vpc & more","provider":"google","description":"wrong","source":"https://example.com/wrong.git"}},
			{"type":"module","id":"mod-1","attributes":{"name":"vpc & more","provider":"aws","description":"VPC","source":"https://example.com/vpc.git","folder":"/modules/vpc/","tagPrefix":"vpc/"},
			 "relationships":{"vcs":{"data":{"type":"vcs","id":"vcs-1"}},"ssh":{"data":null}}}
		]}`)
	})

	resp := readModuleDataSource(t, "vpc & more", "aws", handler)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	var state ModuleDataSourceModel
	if diags := resp.State.Get(context.Background(), &state); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}
	if state.ID.ValueString() != "mod-1" || state.OrganizationId.ValueString() != "org-1" ||
		state.Name.ValueString() != "vpc & more" || state.ProviderName.ValueString() != "aws" ||
		state.Description.ValueString() != "VPC" || state.Source.ValueString() != "https://example.com/vpc.git" ||
		state.VcsId.ValueString() != "vcs-1" || state.Folder.ValueString() != "/modules/vpc/" ||
		state.TagPrefix.ValueString() != "vpc/" {
		t.Errorf("unexpected state: %+v", state)
	}
	if !state.SshId.IsNull() {
		t.Errorf("ssh_id should be null, got %q", state.SshId.ValueString())
	}
}

func TestModuleDataSource_ReadWithoutOptionalAttributes(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data":[{"type":"module","id":"mod-1","attributes":{"name":"vpc","provider":"aws","description":"VPC","source":"https://example.com/vpc.git"}}]}`)
	})

	resp := readModuleDataSource(t, "vpc", "aws", handler)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	var state ModuleDataSourceModel
	if diags := resp.State.Get(context.Background(), &state); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}
	if !state.VcsId.IsNull() || !state.SshId.IsNull() || !state.Folder.IsNull() || !state.TagPrefix.IsNull() {
		t.Errorf("optional attributes should be null: %+v", state)
	}
}

func TestModuleDataSource_ReadErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		summary string
	}{
		{"not found", http.StatusOK, `{"data":[]}`, "Module not found"},
		{"other provider", http.StatusOK, `{"data":[{"type":"module","id":"1","attributes":{"name":"vpc","provider":"google"}}]}`, "Module not found"},
		{"duplicate", http.StatusOK, `{"data":[{"type":"module","id":"1","attributes":{"name":"vpc","provider":"aws"}},{"type":"module","id":"2","attributes":{"name":"vpc","provider":"aws"}}]}`, "Multiple modules found"},
		{"api error", http.StatusForbidden, `{"errors":[{"detail":"forbidden"}]}`, "Error reading module: 403 Forbidden"},
		{"invalid payload", http.StatusOK, `not json`, "Unable to unmarshal module payload"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			})
			resp := readModuleDataSource(t, "vpc", "aws", handler)
			if !resp.Diagnostics.HasError() {
				t.Fatal("expected an error diagnostic")
			}
			if got := resp.Diagnostics.Errors()[0].Summary(); got != test.summary {
				t.Errorf("diagnostic = %q, want %q", got, test.summary)
			}
		})
	}
}
