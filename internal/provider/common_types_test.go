package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func strPtr(s string) *string { return &s }

// --- roleToState ---

func TestRoleToState_NilIsNull(t *testing.T) {
	got := roleToState(nil)
	if !got.IsNull() {
		t.Errorf("expected null for nil input, got %q", got.ValueString())
	}
}

func TestRoleToState_EmptyStringIsNull(t *testing.T) {
	got := roleToState(strPtr(""))
	if !got.IsNull() {
		t.Errorf("expected null for empty string, got %q", got.ValueString())
	}
}

func TestRoleToState_ValidRolePreserved(t *testing.T) {
	for _, role := range []string{"admin", "write", "plan", "read", "custom"} {
		got := roleToState(strPtr(role))
		if got.IsNull() {
			t.Errorf("role %q: expected non-null", role)
		}
		if got.ValueString() != role {
			t.Errorf("role %q: got %q", role, got.ValueString())
		}
	}
}

// --- resolveJobFlag ---

func TestResolveJobFlag_ExplicitTrueOverridesFalseInherit(t *testing.T) {
	got := resolveJobFlag(types.BoolValue(true), types.BoolValue(false))
	if !got {
		t.Error("expected true: explicit true should override false inherit")
	}
}

func TestResolveJobFlag_ExplicitFalseOverridesTrueInherit(t *testing.T) {
	got := resolveJobFlag(types.BoolValue(false), types.BoolValue(true))
	if got {
		t.Error("expected false: explicit false should override true inherit")
	}
}

func TestResolveJobFlag_NullExplicitInheritsTrue(t *testing.T) {
	got := resolveJobFlag(types.BoolNull(), types.BoolValue(true))
	if !got {
		t.Error("expected true: null explicit should inherit true from manage_job")
	}
}

func TestResolveJobFlag_NullExplicitInheritsFalse(t *testing.T) {
	got := resolveJobFlag(types.BoolNull(), types.BoolValue(false))
	if got {
		t.Error("expected false: null explicit should inherit false from manage_job")
	}
}

func TestResolveJobFlag_UnknownExplicitInheritsTrue(t *testing.T) {
	got := resolveJobFlag(types.BoolUnknown(), types.BoolValue(true))
	if !got {
		t.Error("expected true: unknown explicit should inherit from manage_job")
	}
}

func TestResolveJobFlag_ExplicitTrueWithTrueInherit(t *testing.T) {
	got := resolveJobFlag(types.BoolValue(true), types.BoolValue(true))
	if !got {
		t.Error("expected true: explicit true should return true regardless of inherit")
	}
}

func TestResolveJobFlag_ExplicitFalseWithFalseInherit(t *testing.T) {
	got := resolveJobFlag(types.BoolValue(false), types.BoolValue(false))
	if got {
		t.Error("expected false")
	}
}

// --- rbacRoleConflictValidator ---

// validatorTestSchema returns a minimal schema containing the three attributes
// the validator inspects: role, plan_job, and approve_job.
func validatorTestSchema() schema.Schema {
	return schema.Schema{
		Attributes: map[string]schema.Attribute{
			"role":        schema.StringAttribute{Optional: true},
			"plan_job":    schema.BoolAttribute{Optional: true},
			"approve_job": schema.BoolAttribute{Optional: true},
		},
	}
}

// validatorTestObjectType returns the tftypes.Object matching validatorTestSchema.
func validatorTestObjectType() tftypes.Object {
	return tftypes.Object{
		AttributeTypes: map[string]tftypes.Type{
			"role":        tftypes.String,
			"plan_job":    tftypes.Bool,
			"approve_job": tftypes.Bool,
		},
	}
}

func TestRbacRoleConflictValidator(t *testing.T) {
	tests := map[string]struct {
		values     map[string]tftypes.Value
		wantErrors int
	}{
		"custom role with flags — no error": {
			values: map[string]tftypes.Value{
				"role":        tftypes.NewValue(tftypes.String, "custom"),
				"plan_job":    tftypes.NewValue(tftypes.Bool, true),
				"approve_job": tftypes.NewValue(tftypes.Bool, true),
			},
			wantErrors: 0,
		},
		"admin role with plan_job — error": {
			values: map[string]tftypes.Value{
				"role":        tftypes.NewValue(tftypes.String, "admin"),
				"plan_job":    tftypes.NewValue(tftypes.Bool, true),
				"approve_job": tftypes.NewValue(tftypes.Bool, nil),
			},
			wantErrors: 1,
		},
		"read role with both flags — two errors": {
			values: map[string]tftypes.Value{
				"role":        tftypes.NewValue(tftypes.String, "read"),
				"plan_job":    tftypes.NewValue(tftypes.Bool, false),
				"approve_job": tftypes.NewValue(tftypes.Bool, false),
			},
			wantErrors: 2,
		},
		"role unset with flags — no error": {
			values: map[string]tftypes.Value{
				"role":        tftypes.NewValue(tftypes.String, nil),
				"plan_job":    tftypes.NewValue(tftypes.Bool, true),
				"approve_job": tftypes.NewValue(tftypes.Bool, true),
			},
			wantErrors: 0,
		},
		"read role with no flags — no error": {
			values: map[string]tftypes.Value{
				"role":        tftypes.NewValue(tftypes.String, "read"),
				"plan_job":    tftypes.NewValue(tftypes.Bool, nil),
				"approve_job": tftypes.NewValue(tftypes.Bool, nil),
			},
			wantErrors: 0,
		},
		"write role with approve_job only — one error": {
			values: map[string]tftypes.Value{
				"role":        tftypes.NewValue(tftypes.String, "write"),
				"plan_job":    tftypes.NewValue(tftypes.Bool, nil),
				"approve_job": tftypes.NewValue(tftypes.Bool, true),
			},
			wantErrors: 1,
		},
		"all null — no error": {
			values: map[string]tftypes.Value{
				"role":        tftypes.NewValue(tftypes.String, nil),
				"plan_job":    tftypes.NewValue(tftypes.Bool, nil),
				"approve_job": tftypes.NewValue(tftypes.Bool, nil),
			},
			wantErrors: 0,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			s := validatorTestSchema()
			req := resource.ValidateConfigRequest{
				Config: tfsdk.Config{
					Schema: s,
					Raw:    tftypes.NewValue(validatorTestObjectType(), tc.values),
				},
			}
			resp := &resource.ValidateConfigResponse{}

			v := rbacRoleConflictValidator{}
			v.ValidateResource(context.Background(), req, resp)

			if got := resp.Diagnostics.ErrorsCount(); got != tc.wantErrors {
				t.Errorf("expected %d errors, got %d; diagnostics: %v", tc.wantErrors, got, resp.Diagnostics)
			}
		})
	}
}
