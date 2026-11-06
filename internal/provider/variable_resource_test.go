package provider

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const variableTestSecret = "s3cr3t-value"

// variableResource is the part of the workspace and organization variable
// resources these tests drive.
type variableResource interface {
	resource.ResourceWithConfigValidators
	resource.ResourceWithValidateConfig
}

type variableTestCase struct {
	name string
	// newResource builds the resource pointed at the test server ("" for no server).
	newResource func(client *http.Client, endpoint string) variableResource
	// basePath is the collection path; the variable itself is basePath + "/var-1".
	basePath string
	jsonType string
	ids      map[string]tftypes.Value
}

func variableTestCases() []variableTestCase {
	return []variableTestCase{
		{
			name: "workspace_variable",
			newResource: func(c *http.Client, endpoint string) variableResource {
				return &WorkspaceVariableResource{client: c, endpoint: endpoint, token: "test-token"}
			},
			basePath: "/api/v1/organization/org-1/workspace/ws-1/variable",
			jsonType: "variable",
			ids: map[string]tftypes.Value{
				"organization_id": tftypes.NewValue(tftypes.String, "org-1"),
				"workspace_id":    tftypes.NewValue(tftypes.String, "ws-1"),
			},
		},
		{
			name: "organization_variable",
			newResource: func(c *http.Client, endpoint string) variableResource {
				return &OrganizationVariableResource{client: c, endpoint: endpoint, token: "test-token"}
			},
			basePath: "/api/v1/organization/org-1/globalvar",
			jsonType: "globalvar",
			ids: map[string]tftypes.Value{
				"organization_id": tftypes.NewValue(tftypes.String, "org-1"),
			},
		},
	}
}

func (tc variableTestCase) schemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	var schemaResp resource.SchemaResponse
	tc.newResource(nil, "").Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics building schema: %v", schemaResp.Diagnostics)
	}

	objType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("expected schema type to be a tftypes.Object")
	}

	return schemaResp.Schema, objType
}

// server answers every call with a sensitive variable, as Terrakube does: no
// value in the response. It records the last request body.
func (tc variableTestCase) server(t *testing.T, captured *[]byte) *httptest.Server {
	t.Helper()

	response := `{"data":{"type":"` + tc.jsonType + `","id":"var-1","attributes":{` +
		`"key":"TF_TOKEN","value":"","description":"token","category":"ENV","sensitive":true,"hcl":false}}}`

	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, req *http.Request) {
		if req.Body != nil {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("reading request body: %v", err)
			}
			if len(body) > 0 {
				*captured = body
			}
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		fmt.Fprint(w, response)
	}
	mux.HandleFunc(tc.basePath, handler)
	mux.HandleFunc(tc.basePath+"/var-1", handler)

	return httptest.NewServer(mux)
}

func (tc variableTestCase) values(objType tftypes.Object, overrides map[string]tftypes.Value) tftypes.Value {
	values := map[string]tftypes.Value{
		"key":         tftypes.NewValue(tftypes.String, "TF_TOKEN"),
		"description": tftypes.NewValue(tftypes.String, "token"),
		"category":    tftypes.NewValue(tftypes.String, "ENV"),
		"sensitive":   tftypes.NewValue(tftypes.Bool, true),
		"hcl":         tftypes.NewValue(tftypes.Bool, false),
	}
	maps.Copy(values, tc.ids)
	maps.Copy(values, overrides)
	return buildObjectValue(objType, values)
}

func stateString(t *testing.T, ctx context.Context, state tfsdk.State, name string) types.String {
	t.Helper()

	var v types.String
	if diags := state.GetAttribute(ctx, path.Root(name), &v); diags.HasError() {
		t.Fatalf("reading %s from state: %v", name, diags)
	}
	return v
}

func stateInt64(t *testing.T, ctx context.Context, state tfsdk.State, name string) types.Int64 {
	t.Helper()

	var v types.Int64
	if diags := state.GetAttribute(ctx, path.Root(name), &v); diags.HasError() {
		t.Fatalf("reading %s from state: %v", name, diags)
	}
	return v
}

func TestVariableResource_Schema(t *testing.T) {
	ctx := context.Background()
	for _, tc := range variableTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := tc.schemaAndType(t, ctx)

			if !s.Attributes["value"].IsOptional() {
				t.Errorf("expected value to be optional")
			}

			valueWo := s.Attributes["value_wo"]
			if !valueWo.IsOptional() || !valueWo.IsSensitive() || !valueWo.IsWriteOnly() {
				t.Errorf("expected value_wo to be optional, sensitive and write-only")
			}

			if !s.Attributes["value_wo_version"].IsOptional() {
				t.Errorf("expected value_wo_version to be optional")
			}
		})
	}
}

func TestVariableResource_Create_ValueWo(t *testing.T) {
	ctx := context.Background()
	for _, tc := range variableTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			s, objType := tc.schemaAndType(t, ctx)

			var captured []byte
			server := tc.server(t, &captured)
			defer server.Close()

			r := tc.newResource(server.Client(), server.URL)

			// Write-only values are null in the plan; only the configuration has them.
			planValue := tc.values(objType, map[string]tftypes.Value{
				"value_wo_version": tftypes.NewValue(tftypes.Number, 1),
			})
			configValue := tc.values(objType, map[string]tftypes.Value{
				"value_wo":         tftypes.NewValue(tftypes.String, variableTestSecret),
				"value_wo_version": tftypes.NewValue(tftypes.Number, 1),
			})

			req := resource.CreateRequest{
				Plan:   tfsdk.Plan{Schema: s, Raw: planValue},
				Config: tfsdk.Config{Schema: s, Raw: configValue},
			}
			resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

			r.Create(ctx, req, resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics)
			}
			if !strings.Contains(string(captured), variableTestSecret) {
				t.Errorf("expected the request body to carry value_wo, got: %s", captured)
			}
			if v := stateString(t, ctx, resp.State, "value"); !v.IsNull() {
				t.Errorf("expected value to stay null in state, got: %q", v.ValueString())
			}
			if !stateString(t, ctx, resp.State, "value_wo").IsNull() {
				t.Errorf("expected value_wo to be null in state")
			}
			if v := stateInt64(t, ctx, resp.State, "value_wo_version"); v.ValueInt64() != 1 {
				t.Errorf("expected value_wo_version 1 in state, got: %d", v.ValueInt64())
			}
			if id := stateString(t, ctx, resp.State, "id"); id.ValueString() != "var-1" {
				t.Errorf("expected ID var-1, got: %q", id.ValueString())
			}
		})
	}
}

func TestVariableResource_Create_Value(t *testing.T) {
	ctx := context.Background()
	for _, tc := range variableTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			s, objType := tc.schemaAndType(t, ctx)

			var captured []byte
			server := tc.server(t, &captured)
			defer server.Close()

			r := tc.newResource(server.Client(), server.URL)

			values := tc.values(objType, map[string]tftypes.Value{
				"value": tftypes.NewValue(tftypes.String, variableTestSecret),
			})
			req := resource.CreateRequest{
				Plan:   tfsdk.Plan{Schema: s, Raw: values},
				Config: tfsdk.Config{Schema: s, Raw: values},
			}
			resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

			r.Create(ctx, req, resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics)
			}
			if !strings.Contains(string(captured), variableTestSecret) {
				t.Errorf("expected the request body to carry value, got: %s", captured)
			}
			// Unchanged behaviour: a sensitive variable keeps the configured value in state.
			if v := stateString(t, ctx, resp.State, "value"); v.ValueString() != variableTestSecret {
				t.Errorf("expected value preserved in state, got: %q", v.ValueString())
			}
		})
	}
}

func TestVariableResource_Update_ValueWo(t *testing.T) {
	ctx := context.Background()
	for _, tc := range variableTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			s, objType := tc.schemaAndType(t, ctx)

			var captured []byte
			server := tc.server(t, &captured)
			defer server.Close()

			r := tc.newResource(server.Client(), server.URL)

			stateValue := tc.values(objType, map[string]tftypes.Value{
				"id":               tftypes.NewValue(tftypes.String, "var-1"),
				"value_wo_version": tftypes.NewValue(tftypes.Number, 1),
			})
			planValue := tc.values(objType, map[string]tftypes.Value{
				"id":               tftypes.NewValue(tftypes.String, "var-1"),
				"value_wo_version": tftypes.NewValue(tftypes.Number, 2),
			})
			configValue := tc.values(objType, map[string]tftypes.Value{
				"value_wo":         tftypes.NewValue(tftypes.String, "rotated-value"),
				"value_wo_version": tftypes.NewValue(tftypes.Number, 2),
			})

			req := resource.UpdateRequest{
				State:  tfsdk.State{Schema: s, Raw: stateValue},
				Plan:   tfsdk.Plan{Schema: s, Raw: planValue},
				Config: tfsdk.Config{Schema: s, Raw: configValue},
			}
			resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s}}

			r.Update(ctx, req, resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("Update returned diagnostics: %v", resp.Diagnostics)
			}
			if !strings.Contains(string(captured), "rotated-value") {
				t.Errorf("expected the PATCH body to carry the new value_wo, got: %s", captured)
			}
			if !stateString(t, ctx, resp.State, "value").IsNull() || !stateString(t, ctx, resp.State, "value_wo").IsNull() {
				t.Errorf("expected value and value_wo to stay null in state")
			}
			if v := stateInt64(t, ctx, resp.State, "value_wo_version"); v.ValueInt64() != 2 {
				t.Errorf("expected value_wo_version 2 in state, got: %d", v.ValueInt64())
			}
		})
	}
}

func TestVariableResource_Read_KeepsNullValue(t *testing.T) {
	ctx := context.Background()
	for _, tc := range variableTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			s, objType := tc.schemaAndType(t, ctx)

			var captured []byte
			server := tc.server(t, &captured)
			defer server.Close()

			r := tc.newResource(server.Client(), server.URL)

			stateValue := tc.values(objType, map[string]tftypes.Value{
				"id": tftypes.NewValue(tftypes.String, "var-1"),
			})
			req := resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: stateValue}}
			resp := &resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: stateValue}}

			r.Read(ctx, req, resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics)
			}
			// A null value must not become "" here, or it shows as a diff.
			if v := stateString(t, ctx, resp.State, "value"); !v.IsNull() {
				t.Errorf("expected value to stay null after read, got: %q", v.ValueString())
			}
		})
	}
}

func TestVariableResource_ValidateConfig(t *testing.T) {
	ctx := context.Background()
	cases := map[string]struct {
		overrides map[string]tftypes.Value
		wantError bool
	}{
		"value_wo with sensitive=false": {
			overrides: map[string]tftypes.Value{
				"value_wo":  tftypes.NewValue(tftypes.String, variableTestSecret),
				"sensitive": tftypes.NewValue(tftypes.Bool, false),
			},
			wantError: true,
		},
		"value_wo with sensitive=true": {
			overrides: map[string]tftypes.Value{
				"value_wo": tftypes.NewValue(tftypes.String, variableTestSecret),
			},
		},
		"value with sensitive=false": {
			overrides: map[string]tftypes.Value{
				"value":     tftypes.NewValue(tftypes.String, "plain"),
				"sensitive": tftypes.NewValue(tftypes.Bool, false),
			},
		},
	}

	for _, tc := range variableTestCases() {
		s, objType := tc.schemaAndType(t, ctx)
		r := tc.newResource(nil, "")

		for name, c := range cases {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				req := resource.ValidateConfigRequest{
					Config: tfsdk.Config{Schema: s, Raw: tc.values(objType, c.overrides)},
				}
				resp := &resource.ValidateConfigResponse{}

				r.ValidateConfig(ctx, req, resp)

				if resp.Diagnostics.HasError() != c.wantError {
					t.Errorf("expected error=%v, got diagnostics: %v", c.wantError, resp.Diagnostics)
				}
			})
		}
	}
}

func TestVariableResource_ExactlyOneValue(t *testing.T) {
	ctx := context.Background()
	cases := map[string]struct {
		overrides map[string]tftypes.Value
		wantError bool
	}{
		"neither value nor value_wo": {wantError: true},
		"both value and value_wo": {
			overrides: map[string]tftypes.Value{
				"value":    tftypes.NewValue(tftypes.String, "a"),
				"value_wo": tftypes.NewValue(tftypes.String, "b"),
			},
			wantError: true,
		},
		"only value": {
			overrides: map[string]tftypes.Value{
				"value": tftypes.NewValue(tftypes.String, "a"),
			},
		},
		"only value_wo": {
			overrides: map[string]tftypes.Value{
				"value_wo": tftypes.NewValue(tftypes.String, "b"),
			},
		},
	}

	for _, tc := range variableTestCases() {
		s, objType := tc.schemaAndType(t, ctx)
		r := tc.newResource(nil, "")

		for name, c := range cases {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				req := resource.ValidateConfigRequest{
					Config: tfsdk.Config{Schema: s, Raw: tc.values(objType, c.overrides)},
				}

				var gotError bool
				for _, v := range r.ConfigValidators(ctx) {
					resp := &resource.ValidateConfigResponse{}
					v.ValidateResource(ctx, req, resp)
					gotError = gotError || resp.Diagnostics.HasError()
				}

				if gotError != c.wantError {
					t.Errorf("expected error=%v, got %v", c.wantError, gotError)
				}
			})
		}
	}
}
