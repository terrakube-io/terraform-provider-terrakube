package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestLaunch234ResourcePermissions(t *testing.T) {
	tests := []struct {
		name, entity string
		flags        map[string]string
		makeResource func(*httptest.Server) resource.Resource
	}{
		{"team", "team", map[string]string{"destroy_job": "destroyJob"}, func(s *httptest.Server) resource.Resource { return &TeamResource{client: s.Client(), endpoint: s.URL} }},
		{"workspace_access", "access", map[string]string{"destroy_job": "destroyJob"}, func(s *httptest.Server) resource.Resource {
			return &WorkspaceAccessResource{client: s.Client(), endpoint: s.URL}
		}},
		{"project_access", "project_access", map[string]string{"destroy_job": "destroyJob"}, func(s *httptest.Server) resource.Resource {
			return &ProjectAccessResource{client: s.Client(), endpoint: s.URL}
		}},
		{"workspace_cli", "workspace", map[string]string{"restrict_apply_to_configured_branch": "restrictApplyToConfiguredBranch", "require_approve_to_apply": "requireApproveToApply"}, func(s *httptest.Server) resource.Resource {
			return &WorkspaceCliResource{client: s.Client(), endpoint: s.URL}
		}},
		{"workspace_vcs", "workspace", map[string]string{"restrict_apply_to_configured_branch": "restrictApplyToConfiguredBranch", "require_approve_to_apply": "requireApproveToApply"}, func(s *httptest.Server) resource.Resource {
			return &WorkspaceVcsResource{client: s.Client(), endpoint: s.URL}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []string{"false", "true", "omitted", "unknown"} {
				t.Run(mode, func(t *testing.T) {
					ctx := context.Background()
					remote := map[string]any{"name": "test"}
					var sent map[string]any
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
						if req.Method == http.MethodPost || req.Method == http.MethodPatch {
							var payload struct {
								Data struct {
									Attributes map[string]any `json:"attributes"`
								} `json:"data"`
							}
							if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
								t.Error(err)
							}
							sent = payload.Data.Attributes
							for k, v := range sent {
								remote[k] = v
							}
						}
						w.Header().Set("Content-Type", "application/vnd.api+json")
						_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"type": tc.entity, "id": "test-id", "attributes": remote}})
					}))
					defer server.Close()
					r := tc.makeResource(server)
					var sr resource.SchemaResponse
					r.Schema(ctx, resource.SchemaRequest{}, &sr)
					s := sr.Schema
					obj := s.Type().TerraformType(ctx).(tftypes.Object)
					values := map[string]tftypes.Value{"name": tftypes.NewValue(tftypes.String, "test"), "organization_id": tftypes.NewValue(tftypes.String, "org"), "workspace_id": tftypes.NewValue(tftypes.String, "ws"), "project_id": tftypes.NewValue(tftypes.String, "project")}
					for attr := range tc.flags {
						switch mode {
						case "false":
							values[attr] = tftypes.NewValue(tftypes.Bool, false)
						case "true":
							values[attr] = tftypes.NewValue(tftypes.Bool, true)
						case "unknown":
							values[attr] = tftypes.NewValue(tftypes.Bool, tftypes.UnknownValue)
						}
					}
					plan := tfsdk.Plan{Schema: s, Raw: buildObjectValue(obj, values)}
					created := resource.CreateResponse{State: tfsdk.State{Schema: s}}
					r.Create(ctx, resource.CreateRequest{Plan: plan}, &created)
					if created.Diagnostics.HasError() {
						t.Fatal(created.Diagnostics)
					}
					for attr, api := range tc.flags {
						got, exists := sent[api]
						if mode == "omitted" || mode == "unknown" {
							if exists {
								t.Fatalf("unexpected %s in payload", api)
							}
						} else if !exists || got != (mode == "true") {
							t.Fatalf("%s = %v", api, got)
						}
						var stateValue bool
						if mode == "false" || mode == "true" {
							if d := created.State.GetAttribute(ctx, path.Root(attr), &stateValue); d.HasError() {
								t.Fatal(d)
							}
							if stateValue != (mode == "true") {
								t.Fatalf("unexpected state for %s", attr)
							}
						}
						// Simulate remote values and refresh, including import-style reads.
						remote[api] = true
					}
					refreshed := resource.ReadResponse{State: created.State}
					r.Read(ctx, resource.ReadRequest{State: created.State}, &refreshed)
					if refreshed.Diagnostics.HasError() {
						t.Fatal(refreshed.Diagnostics)
					}
					for attr := range tc.flags {
						var v bool
						if d := refreshed.State.GetAttribute(ctx, path.Root(attr), &v); d.HasError() {
							t.Fatal(d)
						}
						if !v {
							t.Fatalf("refresh did not read %s", attr)
						}
					}
					// A configured false must be transmitted. An omitted workspace guard must
					// remain absent even when refreshed state contains true.
					updateValues := map[string]tftypes.Value{}
					for k, v := range values {
						updateValues[k] = v
					}
					configValues := map[string]tftypes.Value{}
					for attr := range tc.flags {
						updateValues[attr] = tftypes.NewValue(tftypes.Bool, mode == "false")
						configValues[attr] = tftypes.NewValue(tftypes.Bool, mode == "false")
						if (mode == "omitted" || mode == "unknown") && tc.entity != "workspace" {
							updateValues[attr] = tftypes.NewValue(tftypes.Bool, nil)
							delete(configValues, attr)
						}
						if mode == "omitted" && tc.entity == "workspace" {
							updateValues[attr] = tftypes.NewValue(tftypes.Bool, true)
							delete(configValues, attr)
						}
					}
					updated := resource.UpdateResponse{State: tfsdk.State{Schema: s}}
					r.Update(ctx, resource.UpdateRequest{State: refreshed.State, Plan: tfsdk.Plan{Schema: s, Raw: buildObjectValue(obj, updateValues)}, Config: tfsdk.Config{Schema: s, Raw: buildObjectValue(obj, configValues)}}, &updated)
					if updated.Diagnostics.HasError() {
						t.Fatal(updated.Diagnostics)
					}
					for _, api := range tc.flags {
						got, exists := sent[api]
						if mode == "omitted" && tc.entity == "workspace" {
							if exists {
								t.Fatalf("unconfigured guard %s sent during update", api)
							}
						} else if (mode == "omitted" || mode == "unknown") && tc.entity != "workspace" {
							if exists {
								t.Fatalf("unconfigured permission %s sent during update", api)
							}
						} else if !exists || got != (mode == "false") {
							t.Fatalf("update %s = %v, present %v", api, got, exists)
						}
					}
				})
			}
		})
	}
}

func TestTeamDataSourceRBAC(t *testing.T) {
	for _, attrs := range []string{`"role":"custom","planJob":true,"approveJob":false,"destroyJob":false`, `"manageJob":true`} {
		t.Run(attrs, func(t *testing.T) {
			ctx := context.Background()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/organization" {
					_, _ = w.Write([]byte(`{"data":[{"type":"organization","id":"org","attributes":{"name":"test"}}]}`))
					return
				}
				_, _ = w.Write([]byte(`{"data":[{"type":"team","id":"team","attributes":{"name":"test",` + attrs + `}}]}`))
			}))
			defer server.Close()
			d := &TeamDataSource{client: server.Client(), endpoint: server.URL}
			var sr datasource.SchemaResponse
			d.Schema(ctx, datasource.SchemaRequest{}, &sr)
			obj := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
			resp := datasource.ReadResponse{State: tfsdk.State{Schema: sr.Schema}}
			d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: sr.Schema, Raw: buildObjectValue(obj, map[string]tftypes.Value{"name": tftypes.NewValue(tftypes.String, "test"), "organization": tftypes.NewValue(tftypes.String, "test")})}}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			var state TeamDataSourceModel
			if diags := resp.State.Get(ctx, &state); diags.HasError() {
				t.Fatal(diags)
			}
			if attrs == `"manageJob":true` {
				if !state.Role.IsNull() || !state.DestroyJob.IsNull() {
					t.Fatalf("older API should leave new fields null: %+v", state)
				}
			} else if state.Role.ValueString() != "custom" || !state.PlanJob.ValueBool() || state.ApproveJob.ValueBool() || state.DestroyJob.IsNull() || state.DestroyJob.ValueBool() {
				t.Fatalf("unexpected RBAC values: %+v", state)
			}
		})
	}
}
