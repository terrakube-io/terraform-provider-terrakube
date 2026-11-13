package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

var (
	nullString   = tftypes.NewValue(tftypes.String, nil)
	woVersionOne = tftypes.NewValue(tftypes.Number, 1)
)

func TestNotificationConfigurationResource_Schema_SigningSecretWo(t *testing.T) {
	ctx := context.Background()
	for _, tc := range notificationConfigurationTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := tc.schemaAndType(t, ctx)

			wo := s.Attributes["signing_secret_wo"]
			if wo == nil || !wo.IsOptional() || !wo.IsSensitive() || !wo.IsWriteOnly() {
				t.Fatalf("expected signing_secret_wo to be optional, sensitive and write-only")
			}
			version, ok := s.Attributes["signing_secret_wo_version"].(schema.Int64Attribute)
			if !ok || !version.IsOptional() {
				t.Fatalf("expected signing_secret_wo_version to be an optional Int64Attribute")
			}
			if len(version.PlanModifiers) != 0 {
				t.Errorf("expected signing_secret_wo_version to have no plan modifiers (a bump must not replace)")
			}
		})
	}
}

func TestNotificationConfigurationResource_Create_SigningSecretWo(t *testing.T) {
	ctx := context.Background()
	for _, tc := range notificationConfigurationTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			s, objType := tc.schemaAndType(t, ctx)

			var captured []byte
			server := notificationConfigurationServer(t, &captured)
			defer server.Close()

			// Write-only values are null in the plan; only the configuration has them.
			plan := tc.values(objType, map[string]tftypes.Value{
				"signing_secret":            nullString,
				"signing_secret_wo_version": woVersionOne,
			})
			config := tc.values(objType, map[string]tftypes.Value{
				"signing_secret":            nullString,
				"signing_secret_wo":         tftypes.NewValue(tftypes.String, notificationTestSigningSecret),
				"signing_secret_wo_version": woVersionOne,
			})

			req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: plan}, Config: tfsdk.Config{Schema: s, Raw: config}}
			resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

			tc.newResource(server).Create(ctx, req, resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics)
			}
			if !strings.Contains(string(captured), notificationTestSigningSecret) {
				t.Errorf("expected the POST body to carry signing_secret_wo, got: %s", captured)
			}
			for _, attr := range []string{"signing_secret", "signing_secret_wo"} {
				var got types.String
				resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root(attr), &got)...)
				if !got.IsNull() {
					t.Errorf("expected %s to stay null in state, got: %v", attr, got)
				}
			}
		})
	}
}

func notificationConfigurationUpdate(t *testing.T, ctx context.Context, tc notificationConfigurationTestCase, s schema.Schema, state, plan, config tftypes.Value) []byte {
	t.Helper()

	var captured []byte
	server := notificationConfigurationServer(t, &captured)
	defer server.Close()

	req := resource.UpdateRequest{
		State:  tfsdk.State{Schema: s, Raw: state},
		Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		Config: tfsdk.Config{Schema: s, Raw: config},
	}
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s}}

	tc.newResource(server).Update(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Update returned diagnostics: %v", resp.Diagnostics)
	}
	var wo types.String
	resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root("signing_secret_wo"), &wo)...)
	if !wo.IsNull() {
		t.Errorf("expected signing_secret_wo to stay null in state, got: %v", wo)
	}
	return captured
}

// Every Update sends signing_secret_wo from the configuration: on an unrelated change, on a
// version bump (rotation) and when moving from signing_secret to signing_secret_wo.
func TestNotificationConfigurationResource_Update_SendsSigningSecretWo(t *testing.T) {
	ctx := context.Background()
	id := map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, "cfg-1")}

	cases := map[string]struct {
		state, plan, config map[string]tftypes.Value
		want                string
	}{
		"description change": {
			state: map[string]tftypes.Value{"signing_secret": nullString, "signing_secret_wo_version": woVersionOne},
			plan: map[string]tftypes.Value{
				"signing_secret": nullString, "signing_secret_wo_version": woVersionOne,
				"description": tftypes.NewValue(tftypes.String, "new description"),
			},
			config: map[string]tftypes.Value{
				"signing_secret": nullString, "signing_secret_wo_version": woVersionOne,
				"signing_secret_wo": tftypes.NewValue(tftypes.String, notificationTestSigningSecret),
				"description":       tftypes.NewValue(tftypes.String, "new description"),
			},
			want: notificationTestSigningSecret,
		},
		"version bump": {
			state: map[string]tftypes.Value{"signing_secret": nullString, "signing_secret_wo_version": woVersionOne},
			plan: map[string]tftypes.Value{
				"signing_secret": nullString, "signing_secret_wo_version": tftypes.NewValue(tftypes.Number, 2),
			},
			config: map[string]tftypes.Value{
				"signing_secret": nullString, "signing_secret_wo_version": tftypes.NewValue(tftypes.Number, 2),
				"signing_secret_wo": tftypes.NewValue(tftypes.String, "rotated-secret"),
			},
			want: "rotated-secret",
		},
		"moved to signing_secret_wo": {
			state: map[string]tftypes.Value{},
			plan:  map[string]tftypes.Value{"signing_secret": nullString, "signing_secret_wo_version": woVersionOne},
			config: map[string]tftypes.Value{
				"signing_secret": nullString, "signing_secret_wo_version": woVersionOne,
				"signing_secret_wo": tftypes.NewValue(tftypes.String, "moved-secret"),
			},
			want: "moved-secret",
		},
	}

	for _, tc := range notificationConfigurationTestCases() {
		s, objType := tc.schemaAndType(t, ctx)
		for name, c := range cases {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				with := func(m map[string]tftypes.Value) tftypes.Value {
					merged := map[string]tftypes.Value{}
					for k, v := range id {
						merged[k] = v
					}
					for k, v := range m {
						merged[k] = v
					}
					return tc.values(objType, merged)
				}

				captured := notificationConfigurationUpdate(t, ctx, tc, s, with(c.state), with(c.plan), with(c.config))
				if !strings.Contains(string(captured), c.want) {
					t.Errorf("expected the PATCH body to carry %q, got: %s", c.want, captured)
				}
			})
		}
	}
}

// With no secret at all, signingSecret stays out of the request as before (omitempty).
func TestNotificationConfigurationResource_Create_NoSigningSecret(t *testing.T) {
	ctx := context.Background()
	for _, tc := range notificationConfigurationTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			s, objType := tc.schemaAndType(t, ctx)

			var captured []byte
			server := notificationConfigurationServer(t, &captured)
			defer server.Close()

			plan := tc.values(objType, map[string]tftypes.Value{"signing_secret": nullString})
			req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: plan}, Config: tfsdk.Config{Schema: s, Raw: plan}}
			resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

			tc.newResource(server).Create(ctx, req, resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics)
			}
			if strings.Contains(string(captured), "signingSecret") {
				t.Errorf("expected no signingSecret in the POST body, got: %s", captured)
			}
		})
	}
}

func TestNotificationConfigurationResource_SigningSecretValidators(t *testing.T) {
	ctx := context.Background()

	cases := map[string]struct {
		overrides map[string]tftypes.Value
		wantError bool
	}{
		"both": {
			overrides: map[string]tftypes.Value{
				"signing_secret_wo": tftypes.NewValue(tftypes.String, "b"),
			},
			wantError: true,
		},
		"neither":             {overrides: map[string]tftypes.Value{"signing_secret": nullString}},
		"only signing_secret": {},
		"only signing_secret_wo": {
			overrides: map[string]tftypes.Value{
				"signing_secret":    nullString,
				"signing_secret_wo": tftypes.NewValue(tftypes.String, "b"),
			},
		},
	}

	for _, tc := range notificationConfigurationTestCases() {
		s, objType := tc.schemaAndType(t, ctx)
		r, ok := tc.newResource(nil).(resource.ResourceWithConfigValidators)
		if !ok {
			t.Fatalf("%s: expected the resource to implement ResourceWithConfigValidators", tc.name)
		}

		for name, c := range cases {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				req := resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: s, Raw: tc.values(objType, c.overrides)}}

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

		t.Run(tc.name+"/version without signing_secret_wo", func(t *testing.T) {
			attr, ok := s.Attributes["signing_secret_wo_version"].(schema.Int64Attribute)
			if !ok {
				t.Fatalf("expected signing_secret_wo_version to be an Int64Attribute")
			}
			req := validator.Int64Request{
				Path:           path.Root("signing_secret_wo_version"),
				PathExpression: path.MatchRoot("signing_secret_wo_version"),
				ConfigValue:    types.Int64Value(1),
				Config: tfsdk.Config{Schema: s, Raw: tc.values(objType, map[string]tftypes.Value{
					"signing_secret_wo_version": woVersionOne,
				})},
			}

			var gotError bool
			for _, v := range attr.Validators {
				resp := &validator.Int64Response{}
				v.ValidateInt64(ctx, req, resp)
				gotError = gotError || resp.Diagnostics.HasError()
			}
			if !gotError {
				t.Errorf("expected signing_secret_wo_version without signing_secret_wo to be rejected")
			}
		})
	}
}
