package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const testWorkspaceOutputJSON = `{"values":{"outputs":{
	"db_password":{"value":"s3cret","sensitive":true},
	"vpc_id":{"value":"vpc-1","sensitive":false}
}}}`

type outputsServerOptions struct {
	orgs      string
	workspace string
	history   string
	output    string
	// emptyOutput serves an empty output file, which output == "" can't express.
	emptyOutput bool
	status      int
}

// newOutputsServer serves the four calls a workspace output lookup makes.
func newOutputsServer(t *testing.T, opts outputsServerOptions) *httptest.Server {
	t.Helper()
	if opts.orgs == "" {
		opts.orgs = `{"data":[{"type":"organization","id":"org-1","attributes":{"name":"acme"}}]}`
	}
	if opts.workspace == "" {
		opts.workspace = `{"data":[{"type":"workspace","id":"ws-1","attributes":{"name":"network"}}]}`
	}
	if opts.output == "" && !opts.emptyOutput {
		opts.output = testWorkspaceOutputJSON
	}

	var server *httptest.Server
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, r *http.Request, body string) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("%s: Authorization = %q", r.URL.Path, got)
		}
		if opts.status != 0 {
			w.WriteHeader(opts.status)
		}
		fmt.Fprint(w, body)
	}
	mux.HandleFunc("/api/v1/organization", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("filter[organization]"); got != "name==acme" {
			t.Errorf("organization filter = %q", got)
		}
		reply(w, r, opts.orgs)
	})
	mux.HandleFunc("/api/v1/organization/org-1/workspace", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("filter[workspace]"); got != "name==network" {
			t.Errorf("workspace filter = %q", got)
		}
		reply(w, r, opts.workspace)
	})
	mux.HandleFunc("/api/v1/organization/org-1/workspace/ws-1/history", func(w http.ResponseWriter, r *http.Request) {
		history := opts.history
		if history == "" {
			history = fmt.Sprintf(`{"data":[{"type":"history","id":"h-1","attributes":{"output":"%s/tfoutput/ws-1/state.json"}}]}`, server.URL)
		}
		reply(w, r, history)
	})
	mux.HandleFunc("/tfoutput/ws-1/state.json", func(w http.ResponseWriter, r *http.Request) {
		reply(w, r, opts.output)
	})

	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func openOutputEphemeralResource(t *testing.T, server *httptest.Server) *ephemeral.OpenResponse {
	t.Helper()
	ctx := context.Background()
	e := &OutputEphemeralResource{client: server.Client(), endpoint: server.URL, token: "test-token"}

	var schemaResp ephemeral.SchemaResponse
	e.Schema(ctx, ephemeral.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("unexpected schema diagnostics: %v", schemaResp.Diagnostics)
	}
	objType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("expected schema type to be a tftypes.Object")
	}

	config := buildObjectValue(objType, map[string]tftypes.Value{
		"organization": tftypes.NewValue(tftypes.String, "acme"),
		"workspace":    tftypes.NewValue(tftypes.String, "network"),
	})
	req := ephemeral.OpenRequest{Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: config}}
	resp := &ephemeral.OpenResponse{Result: tfsdk.EphemeralResultData{Schema: schemaResp.Schema}}
	e.Open(ctx, req, resp)
	return resp
}

// outputAttributes returns the attributes of a dynamic object value.
func outputAttributes(t *testing.T, value types.Dynamic) map[string]string {
	t.Helper()
	obj, ok := value.UnderlyingValue().(types.Object)
	if !ok {
		t.Fatalf("expected an object, got %T", value.UnderlyingValue())
	}
	attrs := map[string]string{}
	for name, v := range obj.Attributes() {
		s, ok := v.(types.String)
		if !ok {
			t.Fatalf("%s: expected a string, got %T", name, v)
		}
		attrs[name] = s.ValueString()
	}
	return attrs
}

func TestOutputEphemeralResource_Schema(t *testing.T) {
	var resp ephemeral.SchemaResponse
	(&OutputEphemeralResource{}).Schema(context.Background(), ephemeral.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if !resp.Schema.Attributes["values"].IsSensitive() {
		t.Error("values must be sensitive")
	}
	if resp.Schema.Attributes["nonsensitive_values"].IsSensitive() {
		t.Error("nonsensitive_values must not be sensitive")
	}
}

func TestOutputEphemeralResource_Open(t *testing.T) {
	resp := openOutputEphemeralResource(t, newOutputsServer(t, outputsServerOptions{}))
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var result OutputEphemeralResourceModel
	if diags := resp.Result.Get(context.Background(), &result); diags.HasError() {
		t.Fatalf("reading result: %v", diags)
	}
	values := outputAttributes(t, result.Values)
	if len(values) != 2 || values["db_password"] != "s3cret" || values["vpc_id"] != "vpc-1" {
		t.Errorf("values = %v", values)
	}
	nonSensitive := outputAttributes(t, result.NonSensitiveValues)
	if len(nonSensitive) != 1 || nonSensitive["vpc_id"] != "vpc-1" {
		t.Errorf("nonsensitive_values = %v", nonSensitive)
	}
}

func TestOutputEphemeralResource_OpenWithoutHistory(t *testing.T) {
	resp := openOutputEphemeralResource(t, newOutputsServer(t, outputsServerOptions{history: `{"data":[]}`}))
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	var result OutputEphemeralResourceModel
	if diags := resp.Result.Get(context.Background(), &result); diags.HasError() {
		t.Fatalf("reading result: %v", diags)
	}
	if !result.Values.IsNull() || !result.NonSensitiveValues.IsNull() {
		t.Errorf("expected null values without history, got %v and %v", result.Values, result.NonSensitiveValues)
	}
}

func TestOutputEphemeralResource_OpenErrors(t *testing.T) {
	tests := []struct {
		name    string
		opts    outputsServerOptions
		summary string
	}{
		{"organization not found", outputsServerOptions{orgs: `{"data":[]}`}, `Organization "acme" not found!`},
		{"workspace not found", outputsServerOptions{workspace: `{"data":[]}`}, `Workspace "network" not found!`},
		{"api error", outputsServerOptions{status: http.StatusForbidden, orgs: `{"errors":[{"detail":"forbidden"}]}`}, "Error reading workspace output: 403 Forbidden"},
		{"output not json", outputsServerOptions{output: `not json`}, "Unable to read workspace output"},
		{"output entry not an object", outputsServerOptions{output: `{"values":{"outputs":{"region":"eu-west-1"}}}`}, "Unable to read workspace output"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp := openOutputEphemeralResource(t, newOutputsServer(t, test.opts))
			if !resp.Diagnostics.HasError() {
				t.Fatal("expected an error diagnostic")
			}
			if got := resp.Diagnostics.Errors()[0].Summary(); got != test.summary {
				t.Errorf("diagnostic = %q, want %q", got, test.summary)
			}
		})
	}
}

// The data source shares the lookup with the ephemeral resource; check it
// still returns the same values.
func TestOutputDataSource_Read(t *testing.T) {
	ctx := context.Background()
	server := newOutputsServer(t, outputsServerOptions{})
	d := &OutputDataSource{client: server.Client(), endpoint: server.URL, token: "test-token"}

	var schemaResp datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
	objType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("expected schema type to be a tftypes.Object")
	}
	config := buildObjectValue(objType, map[string]tftypes.Value{
		"organization": tftypes.NewValue(tftypes.String, "acme"),
		"workspace":    tftypes.NewValue(tftypes.String, "network"),
	})
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: config}}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state OutputDataSourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("reading state: %v", diags)
	}
	if values := outputAttributes(t, state.Values); len(values) != 2 || values["db_password"] != "s3cret" {
		t.Errorf("values = %v", values)
	}
	if nonSensitive := outputAttributes(t, state.NonSensitiveValues); len(nonSensitive) != 1 || nonSensitive["vpc_id"] != "vpc-1" {
		t.Errorf("nonsensitive_values = %v", nonSensitive)
	}
}

// A state without outputs is not an error: the values are null, as without a run. The state
// endpoint answers a missing output file with 200 and an empty body, which counts as no outputs too.
func TestOutputEphemeralResource_OpenWithoutOutputs(t *testing.T) {
	for _, opts := range []outputsServerOptions{{output: `{}`}, {output: `{"values":{}}`}, {emptyOutput: true}, {output: "\n"}} {
		output := opts.output
		resp := openOutputEphemeralResource(t, newOutputsServer(t, opts))
		if resp.Diagnostics.HasError() {
			t.Fatalf("%s: unexpected diagnostics: %v", output, resp.Diagnostics)
		}
		var result OutputEphemeralResourceModel
		if diags := resp.Result.Get(context.Background(), &result); diags.HasError() {
			t.Fatalf("%s: reading result: %v", output, diags)
		}
		if !result.Values.IsNull() || !result.NonSensitiveValues.IsNull() {
			t.Errorf("%s: expected null values, got %v and %v", output, result.Values, result.NonSensitiveValues)
		}
	}
}

// The data source shares the lookup, so it reports the same errors.
func TestOutputDataSource_ReadError(t *testing.T) {
	ctx := context.Background()
	server := newOutputsServer(t, outputsServerOptions{output: `not json`})
	d := &OutputDataSource{client: server.Client(), endpoint: server.URL, token: "test-token"}

	var schemaResp datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
	objType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("expected schema type to be a tftypes.Object")
	}
	config := buildObjectValue(objType, map[string]tftypes.Value{
		"organization": tftypes.NewValue(tftypes.String, "acme"),
		"workspace":    tftypes.NewValue(tftypes.String, "network"),
	})
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: config}}, resp)
	if !resp.Diagnostics.HasError() || resp.Diagnostics.Errors()[0].Summary() != "Unable to read workspace output" {
		t.Fatalf("expected an output error, got %v", resp.Diagnostics)
	}
}
