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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const (
	vcsTestSecret = "oauth-s3cr3t"
	vcsTestKey    = "-----BEGIN PRIVATE KEY-----test"
)

func vcsResourceSchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	r := &VcsResource{}
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

// vcsServer answers every call as Terrakube does: no client secret or private
// key in the response. It records the body of the last POST or PATCH.
func vcsServer(t *testing.T, captured *[]byte) *httptest.Server {
	t.Helper()

	base := "/api/v1/organization/org-1/vcs"
	response := `{"data":{"type":"vcs","id":"vcs-1","attributes":{` +
		`"name":"github","description":"desc","vcsType":"GITHUB","connectionType":"OAUTH",` +
		`"clientId":"client-1","endpoint":"https://github.com","apiUrl":"https://api.github.com","status":"PENDING"}}}`

	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		if len(body) > 0 {
			*captured = body
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		fmt.Fprint(w, response)
	}
	mux.HandleFunc(base, handler)
	mux.HandleFunc(base+"/vcs-1", handler)

	return httptest.NewServer(mux)
}

func vcsValues(objType tftypes.Object, overrides map[string]tftypes.Value) tftypes.Value {
	values := map[string]tftypes.Value{
		"organization_id": tftypes.NewValue(tftypes.String, "org-1"),
		"name":            tftypes.NewValue(tftypes.String, "github"),
		"description":     tftypes.NewValue(tftypes.String, "desc"),
		"vcs_type":        tftypes.NewValue(tftypes.String, "GITHUB"),
		"connection_type": tftypes.NewValue(tftypes.String, "OAUTH"),
		"client_id":       tftypes.NewValue(tftypes.String, "client-1"),
		"endpoint":        tftypes.NewValue(tftypes.String, "https://github.com"),
		"api_url":         tftypes.NewValue(tftypes.String, "https://api.github.com"),
		"status":          tftypes.NewValue(tftypes.String, "PENDING"),
		"connect_url":     tftypes.NewValue(tftypes.String, "https://github.com/login"),
	}
	maps.Copy(values, overrides)
	return buildObjectValue(objType, values)
}

func TestVcsResource_Schema(t *testing.T) {
	ctx := context.Background()
	s, _ := vcsResourceSchemaAndType(t, ctx)

	for _, name := range []string{"client_secret_wo", "private_key_wo"} {
		a := s.Attributes[name]
		if !a.IsOptional() || !a.IsSensitive() || !a.IsWriteOnly() {
			t.Errorf("expected %s to be optional, sensitive and write-only", name)
		}
		if !s.Attributes[name+"_version"].IsOptional() {
			t.Errorf("expected %s_version to be optional", name)
		}
	}
}

func TestVcsResource_Create_ClientSecretWo(t *testing.T) {
	ctx := context.Background()
	s, objType := vcsResourceSchemaAndType(t, ctx)

	var captured []byte
	server := vcsServer(t, &captured)
	defer server.Close()

	r := &VcsResource{client: server.Client(), endpoint: server.URL, token: "test-token"}

	// Write-only values are null in the plan; only the configuration has them.
	planValue := vcsValues(objType, map[string]tftypes.Value{
		"client_secret_wo_version": tftypes.NewValue(tftypes.Number, 1),
	})
	configValue := vcsValues(objType, map[string]tftypes.Value{
		"client_secret_wo":         tftypes.NewValue(tftypes.String, vcsTestSecret),
		"client_secret_wo_version": tftypes.NewValue(tftypes.Number, 1),
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
	if !strings.Contains(string(captured), vcsTestSecret) {
		t.Errorf("expected the request body to carry client_secret_wo, got: %s", captured)
	}

	var result VcsResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}
	if !result.ClientSecret.IsNull() || !result.ClientSecretWo.IsNull() {
		t.Errorf("expected client_secret and client_secret_wo to stay null in state")
	}
	if result.ClientSecretWoVersion.ValueInt64() != 1 {
		t.Errorf("expected client_secret_wo_version 1 in state, got: %d", result.ClientSecretWoVersion.ValueInt64())
	}
	if result.ID.ValueString() != "vcs-1" {
		t.Errorf("expected ID vcs-1, got: %q", result.ID.ValueString())
	}
}

// An update that does not touch the secret must still send it: the PATCH
// carries every attribute, and an empty private key would clear it.
func TestVcsResource_Update_KeepsPrivateKeyWo(t *testing.T) {
	ctx := context.Background()
	s, objType := vcsResourceSchemaAndType(t, ctx)

	var captured []byte
	server := vcsServer(t, &captured)
	defer server.Close()

	r := &VcsResource{client: server.Client(), endpoint: server.URL, token: "test-token"}

	stateValue := vcsValues(objType, map[string]tftypes.Value{
		"id":                     tftypes.NewValue(tftypes.String, "vcs-1"),
		"private_key_wo_version": tftypes.NewValue(tftypes.Number, 1),
	})
	planValue := vcsValues(objType, map[string]tftypes.Value{
		"id":                     tftypes.NewValue(tftypes.String, "vcs-1"),
		"description":            tftypes.NewValue(tftypes.String, "new description"),
		"private_key_wo_version": tftypes.NewValue(tftypes.Number, 1),
	})
	configValue := vcsValues(objType, map[string]tftypes.Value{
		"description":            tftypes.NewValue(tftypes.String, "new description"),
		"private_key_wo":         tftypes.NewValue(tftypes.String, vcsTestKey),
		"private_key_wo_version": tftypes.NewValue(tftypes.Number, 1),
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
	if !strings.Contains(string(captured), vcsTestKey) {
		t.Errorf("expected the PATCH body to carry private_key_wo, got: %s", captured)
	}

	var result VcsResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}
	if !result.PrivateKey.IsNull() || !result.PrivateKeyWo.IsNull() {
		t.Errorf("expected private_key and private_key_wo to stay null in state")
	}
}

func TestVcsResource_ConfigValidators(t *testing.T) {
	ctx := context.Background()
	s, objType := vcsResourceSchemaAndType(t, ctx)
	r := &VcsResource{}

	cases := map[string]struct {
		overrides map[string]tftypes.Value
		wantError bool
	}{
		"no secret": {wantError: true},
		"client_secret and client_secret_wo": {
			overrides: map[string]tftypes.Value{
				"client_secret":    tftypes.NewValue(tftypes.String, "a"),
				"client_secret_wo": tftypes.NewValue(tftypes.String, "b"),
			},
			wantError: true,
		},
		"private_key and private_key_wo": {
			overrides: map[string]tftypes.Value{
				"private_key":    tftypes.NewValue(tftypes.String, "a"),
				"private_key_wo": tftypes.NewValue(tftypes.String, "b"),
			},
			wantError: true,
		},
		"only client_secret": {
			overrides: map[string]tftypes.Value{
				"client_secret": tftypes.NewValue(tftypes.String, "a"),
			},
		},
		"only private_key_wo": {
			overrides: map[string]tftypes.Value{
				"private_key_wo": tftypes.NewValue(tftypes.String, "b"),
			},
		},
		"client_secret_wo and private_key_wo": {
			overrides: map[string]tftypes.Value{
				"client_secret_wo": tftypes.NewValue(tftypes.String, "a"),
				"private_key_wo":   tftypes.NewValue(tftypes.String, "b"),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := resource.ValidateConfigRequest{
				Config: tfsdk.Config{Schema: s, Raw: vcsValues(objType, tc.overrides)},
			}

			var gotError bool
			for _, v := range r.ConfigValidators(ctx) {
				resp := &resource.ValidateConfigResponse{}
				v.ValidateResource(ctx, req, resp)
				gotError = gotError || resp.Diagnostics.HasError()
			}

			if gotError != tc.wantError {
				t.Errorf("expected error=%v, got %v", tc.wantError, gotError)
			}
		})
	}
}

// Each secret is replaced when its value changes or is removed, but not when it
// moves to its write-only argument.
func TestVcsResource_SecretRequiresReplace(t *testing.T) {
	ctx := context.Background()
	s, objType := vcsResourceSchemaAndType(t, ctx)

	for _, name := range []string{"client_secret", "private_key"} {
		attr, ok := s.Attributes[name].(schema.StringAttribute)
		if !ok {
			t.Fatalf("expected %s to be a StringAttribute", name)
		}

		stateValue := vcsValues(objType, map[string]tftypes.Value{
			"id": tftypes.NewValue(tftypes.String, "vcs-1"),
			name: tftypes.NewValue(tftypes.String, "old"),
		})

		cases := map[string]struct {
			config      map[string]tftypes.Value
			planned     types.String
			wantReplace bool
		}{
			"changed": {
				config: map[string]tftypes.Value{
					name: tftypes.NewValue(tftypes.String, "new"),
				},
				planned:     types.StringValue("new"),
				wantReplace: true,
			},
			"removed": {
				planned:     types.StringNull(),
				wantReplace: true,
			},
			"moved to " + name + "_wo": {
				config: map[string]tftypes.Value{
					name + "_wo":         tftypes.NewValue(tftypes.String, "old"),
					name + "_wo_version": tftypes.NewValue(tftypes.Number, 1),
				},
				planned: types.StringNull(),
			},
		}

		for caseName, tc := range cases {
			t.Run(name+"/"+caseName, func(t *testing.T) {
				config := vcsValues(objType, tc.config)
				plan := vcsValues(objType, map[string]tftypes.Value{
					"id": tftypes.NewValue(tftypes.String, "vcs-1"),
					name: tftypes.NewValue(tftypes.String, tc.planned.ValueStringPointer()),
				})

				req := planmodifier.StringRequest{
					Path:        path.Root(name),
					Config:      tfsdk.Config{Schema: s, Raw: config},
					ConfigValue: tc.planned,
					State:       tfsdk.State{Schema: s, Raw: stateValue},
					StateValue:  types.StringValue("old"),
					Plan:        tfsdk.Plan{Schema: s, Raw: plan},
					PlanValue:   tc.planned,
				}

				var gotReplace bool
				for _, m := range attr.PlanModifiers {
					resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
					m.PlanModifyString(ctx, req, resp)
					if resp.Diagnostics.HasError() {
						t.Fatalf("plan modifier returned diagnostics: %v", resp.Diagnostics)
					}
					gotReplace = gotReplace || resp.RequiresReplace
				}

				if gotReplace != tc.wantReplace {
					t.Errorf("expected RequiresReplace=%v, got %v", tc.wantReplace, gotReplace)
				}
			})
		}
	}
}

// Bumping *_wo_version is an in-place update that sends the new value.
func TestVcsResource_Update_WoVersionBump(t *testing.T) {
	ctx := context.Background()
	s, objType := vcsResourceSchemaAndType(t, ctx)

	for _, name := range []string{"client_secret", "private_key"} {
		t.Run(name, func(t *testing.T) {
			version, ok := s.Attributes[name+"_wo_version"].(schema.Int64Attribute)
			if !ok {
				t.Fatalf("expected %s_wo_version to be an Int64Attribute", name)
			}
			if len(version.PlanModifiers) != 0 {
				t.Errorf("expected %s_wo_version to have no plan modifiers (a bump must not replace)", name)
			}

			var captured []byte
			server := vcsServer(t, &captured)
			defer server.Close()

			r := &VcsResource{client: server.Client(), endpoint: server.URL, token: "test-token"}

			stateValue := vcsValues(objType, map[string]tftypes.Value{
				"id":                 tftypes.NewValue(tftypes.String, "vcs-1"),
				name + "_wo_version": tftypes.NewValue(tftypes.Number, 1),
			})
			planValue := vcsValues(objType, map[string]tftypes.Value{
				"id":                 tftypes.NewValue(tftypes.String, "vcs-1"),
				name + "_wo_version": tftypes.NewValue(tftypes.Number, 2),
			})
			configValue := vcsValues(objType, map[string]tftypes.Value{
				name + "_wo":         tftypes.NewValue(tftypes.String, "rotated-value"),
				name + "_wo_version": tftypes.NewValue(tftypes.Number, 2),
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
				t.Errorf("expected the PATCH body to carry the new %s_wo, got: %s", name, captured)
			}

			var result VcsResourceModel
			if diags := resp.State.Get(ctx, &result); diags.HasError() {
				t.Fatalf("reading resulting state: %v", diags)
			}
			if result.ID.ValueString() != "vcs-1" {
				t.Errorf("expected ID vcs-1 to be kept, got: %q", result.ID.ValueString())
			}
			if !result.ClientSecretWo.IsNull() || !result.PrivateKeyWo.IsNull() {
				t.Errorf("expected write-only values to stay null in state")
			}
		})
	}
}

func TestVcsResource_WoVersionRequiresWo(t *testing.T) {
	ctx := context.Background()
	s, objType := vcsResourceSchemaAndType(t, ctx)

	for _, name := range []string{"client_secret", "private_key"} {
		t.Run(name, func(t *testing.T) {
			attr, ok := s.Attributes[name+"_wo_version"].(schema.Int64Attribute)
			if !ok {
				t.Fatalf("expected %s_wo_version to be an Int64Attribute", name)
			}

			req := validator.Int64Request{
				Path:           path.Root(name + "_wo_version"),
				PathExpression: path.MatchRoot(name + "_wo_version"),
				ConfigValue:    types.Int64Value(1),
				Config: tfsdk.Config{Schema: s, Raw: vcsValues(objType, map[string]tftypes.Value{
					name:                 tftypes.NewValue(tftypes.String, "plain"),
					name + "_wo_version": tftypes.NewValue(tftypes.Number, 1),
				})},
			}

			var gotError bool
			for _, v := range attr.Validators {
				resp := &validator.Int64Response{}
				v.ValidateInt64(ctx, req, resp)
				gotError = gotError || resp.Diagnostics.HasError()
			}

			if !gotError {
				t.Errorf("expected %s_wo_version without %s_wo to be rejected", name, name)
			}
		})
	}
}
