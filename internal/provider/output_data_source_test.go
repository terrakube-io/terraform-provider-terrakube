package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const outputTestState = `{"values":{"outputs":{
	"vpc_id":      {"value": "vpc-123", "sensitive": false},
	"db_password": {"value": "hunter2", "sensitive": true},
	"replicas":    {"value": 3, "sensitive": false},
	"enabled":     {"value": true, "sensitive": false},
	"subnets":     {"value": ["subnet-a", "subnet-b"], "sensitive": false},
	"tags":        {"value": {"env": "prod", "team": "platform"}, "sensitive": false}
}}}`

// outputHarness is a Terrakube that serves an organization, a workspace, its history and the
// output file the latest history entry points at.
type outputHarness struct {
	t *testing.T

	orgs       string
	workspaces string
	histories  func(serverURL string) string
	outputBody string

	orgStatus       int
	workspaceStatus int
	historyStatus   int
	outputStatus    int

	paths []string
	query map[string]string
}

func newOutputHarness(t *testing.T) *outputHarness {
	t.Helper()
	return &outputHarness{
		t:               t,
		orgs:            `{"data":[{"type":"organization","id":"org-1","attributes":{"name":"my-org"}}]}`,
		workspaces:      `{"data":[{"type":"workspace","id":"ws-1","attributes":{"name":"prod & more"}}]}`,
		histories:       func(u string) string { return historiesJSON(u, "latest", "older") },
		outputBody:      outputTestState,
		orgStatus:       http.StatusOK,
		workspaceStatus: http.StatusOK,
		historyStatus:   http.StatusOK,
		outputStatus:    http.StatusOK,
		query:           map[string]string{},
	}
}

func historiesJSON(serverURL string, files ...string) string {
	items := make([]string, 0, len(files))
	for i, file := range files {
		items = append(items, fmt.Sprintf(`{"type":"history","id":"h-%d","attributes":{"output":"%s/outputs/%s.json"}}`, i, serverURL, file))
	}
	return `{"data":[` + strings.Join(items, ",") + `]}`
}

func (h *outputHarness) read() *datasource.ReadResponse {
	h.t.Helper()
	ctx := context.Background()
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.paths = append(h.paths, r.URL.Path)
		reply := func(status int, body string) {
			w.WriteHeader(status)
			fmt.Fprint(w, body)
		}
		forbidden := `{"errors":[{"detail":"the server said no"}]}`
		switch {
		case r.URL.Path == "/api/v1/organization":
			h.query["organization"] = r.URL.Query().Get("filter")
			if h.orgStatus != http.StatusOK {
				reply(h.orgStatus, forbidden)
				return
			}
			reply(http.StatusOK, h.orgs)
		case r.URL.Path == "/api/v1/organization/org-1/workspace":
			h.query["workspace"] = r.URL.Query().Get("filter[workspace]")
			if h.workspaceStatus != http.StatusOK {
				reply(h.workspaceStatus, forbidden)
				return
			}
			reply(http.StatusOK, h.workspaces)
		case r.URL.Path == "/api/v1/organization/org-1/workspace/ws-1/history":
			h.query["sort"] = r.URL.Query().Get("sort")
			if h.historyStatus != http.StatusOK {
				reply(h.historyStatus, forbidden)
				return
			}
			reply(http.StatusOK, h.histories(serverURL))
		case strings.HasPrefix(r.URL.Path, "/outputs/"):
			if h.outputStatus != http.StatusOK {
				reply(h.outputStatus, forbidden)
				return
			}
			reply(http.StatusOK, h.outputBody)
		default:
			h.t.Errorf("unexpected request: %s", r.URL)
			reply(http.StatusTeapot, "")
		}
	}))
	h.t.Cleanup(server.Close)
	serverURL = server.URL

	d := &OutputDataSource{client: newTestClient(server.Client(), server.URL)}
	var schemaResp datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
	objType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		h.t.Fatal("expected schema type to be a tftypes.Object")
	}
	config := buildObjectValue(objType, map[string]tftypes.Value{
		"organization": tftypes.NewValue(tftypes.String, "my-org"),
		"workspace":    tftypes.NewValue(tftypes.String, "prod & more"),
	})
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: config}}, resp)
	return resp
}

func dynamicAttributes(t *testing.T, resp *datasource.ReadResponse, name string) map[string]attr.Value {
	t.Helper()
	var value types.Dynamic
	if diags := resp.State.GetAttribute(context.Background(), path.Root(name), &value); diags.HasError() {
		t.Fatalf("reading %s: %v", name, diags)
	}
	if value.IsNull() {
		return nil
	}
	object, ok := value.UnderlyingValue().(types.Object)
	if !ok {
		t.Fatalf("%s is %T, want an object", name, value.UnderlyingValue())
	}
	return object.Attributes()
}

func TestOutputDataSource_Read(t *testing.T) {
	h := newOutputHarness(t)
	resp := h.read()
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	// The names are escaped into the filters, and the newest history entry is used.
	if h.query["organization"] != `name=="my-org"` || h.query["workspace"] != `name=="prod & more"` {
		t.Errorf("unexpected filters: %v", h.query)
	}
	if h.query["sort"] != "-createdDate" {
		t.Errorf("the history must be newest first, got sort=%q", h.query["sort"])
	}
	if last := h.paths[len(h.paths)-1]; last != "/outputs/latest.json" {
		t.Errorf("the output file of the newest history entry must be read, got %s", last)
	}

	all := dynamicAttributes(t, resp, "values")
	if len(all) != 6 {
		t.Fatalf("values has %d outputs, want 6: %v", len(all), all)
	}
	text := func(name string) string {
		value, ok := all[name].(types.String)
		if !ok {
			t.Fatalf("%s is %T, want a string", name, all[name])
		}
		return value.ValueString()
	}
	if got := text("vpc_id"); got != "vpc-123" {
		t.Errorf("vpc_id = %q", got)
	}
	if got := text("db_password"); got != "hunter2" {
		t.Errorf("db_password = %q", got)
	}
	replicas, ok := all["replicas"].(types.Number)
	if !ok {
		t.Fatalf("replicas is %T, want a number", all["replicas"])
	}
	if got, _ := replicas.ValueBigFloat().Int64(); got != 3 {
		t.Errorf("replicas = %v", replicas)
	}
	enabled, ok := all["enabled"].(types.Bool)
	if !ok {
		t.Fatalf("enabled is %T, want a bool", all["enabled"])
	}
	if !enabled.ValueBool() {
		t.Errorf("enabled = %v", enabled)
	}
	if !strings.Contains(all["subnets"].String(), "subnet-a") {
		t.Errorf("subnets was not converted: %s", all["subnets"])
	}
	if !strings.Contains(all["tags"].String(), "platform") {
		t.Errorf("tags was not converted: %s", all["tags"])
	}

	// nonsensitive_values leaves out the sensitive output.
	plain := dynamicAttributes(t, resp, "nonsensitive_values")
	if len(plain) != 5 {
		t.Fatalf("nonsensitive_values has %d outputs, want 5: %v", len(plain), plain)
	}
	if _, ok := plain["db_password"]; ok {
		t.Error("the sensitive output must not be in nonsensitive_values")
	}
	if _, ok := plain["vpc_id"]; !ok {
		t.Error("vpc_id should be in nonsensitive_values")
	}
}

func TestOutputDataSource_NoOutputsIsNotAnError(t *testing.T) {
	t.Run("a workspace with no history", func(t *testing.T) {
		h := newOutputHarness(t)
		h.histories = func(string) string { return `{"data":[]}` }
		resp := h.read()
		if resp.Diagnostics.HasError() {
			t.Fatalf("no history is not an error: %v", resp.Diagnostics)
		}
		if dynamicAttributes(t, resp, "values") != nil {
			t.Error("values should be null")
		}
	})
	t.Run("an output file that is empty because it was not uploaded yet", func(t *testing.T) {
		h := newOutputHarness(t)
		h.outputBody = ""
		resp := h.read()
		if resp.Diagnostics.HasError() {
			t.Fatalf("an empty output file is not an error: %v", resp.Diagnostics)
		}
		if dynamicAttributes(t, resp, "values") != nil {
			t.Error("values should be null")
		}
	})
	t.Run("a state with no values", func(t *testing.T) {
		h := newOutputHarness(t)
		h.outputBody = `{"format_version":"1.0"}`
		resp := h.read()
		if resp.Diagnostics.HasError() {
			t.Fatalf("a state without outputs is not an error: %v", resp.Diagnostics)
		}
		if dynamicAttributes(t, resp, "values") != nil {
			t.Error("values should be null")
		}
	})
}

func TestOutputDataSource_ReadErrors(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(h *outputHarness)
		summary string
	}{
		{"organization not found", func(h *outputHarness) { h.orgs = `{"data":[]}` }, "Organization not found"},
		{"organization api error", func(h *outputHarness) { h.orgStatus = http.StatusForbidden }, "Error reading workspace output: 403 Forbidden"},
		{"another organization only", func(h *outputHarness) {
			h.orgs = `{"data":[{"type":"organization","id":"org-9","attributes":{"name":"my-org*"}}]}`
		}, "Organization not found"},
		{"workspace not found", func(h *outputHarness) { h.workspaces = `{"data":[]}` }, "Workspace not found"},
		{"two workspaces", func(h *outputHarness) {
			h.workspaces = `{"data":[` +
				`{"type":"workspace","id":"ws-1","attributes":{"name":"prod & more"}},` +
				`{"type":"workspace","id":"ws-2","attributes":{"name":"prod & more"}}]}`
		}, "Multiple workspace entries found"},
		{"workspace api error", func(h *outputHarness) { h.workspaceStatus = http.StatusInternalServerError }, "Error reading workspace output: 500 Internal Server Error"},
		{"history api error", func(h *outputHarness) { h.historyStatus = http.StatusInternalServerError }, "Error reading workspace output: 500 Internal Server Error"},
		{"output file api error", func(h *outputHarness) { h.outputStatus = http.StatusForbidden }, "Error downloading workspace output: 403 Forbidden"},
		{"output file is not JSON", func(h *outputHarness) { h.outputBody = `not json` }, "Unable to read the workspace output file"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newOutputHarness(t)
			test.setup(h)
			resp := h.read()
			errs := resp.Diagnostics.Errors()
			if len(errs) != 1 {
				t.Fatalf("expected exactly one error, got %d: %v", len(errs), errs)
			}
			if got := errs[0].Summary(); got != test.summary {
				t.Errorf("diagnostic = %q, want %q", got, test.summary)
			}
		})
	}
}
