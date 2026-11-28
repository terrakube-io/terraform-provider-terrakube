package provider

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func protocolList(values ...string) tftypes.Value {
	elements := make([]tftypes.Value, len(values))
	for i, v := range values {
		elements[i] = tftypes.NewValue(tftypes.String, v)
	}
	return tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, elements)
}

func registryProviderVersionCase(create map[string]tftypes.Value) crudResourceCase {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	return crudResourceCase{
		itemType:   "version",
		collection: "/api/v1/organization/org-1/provider/prov-1/version",
		newSource: func(c *http.Client, endpoint string) resource.Resource {
			return &RegistryProviderVersionResource{client: newTestClient(c, endpoint)}
		},
		scope:  map[string]tftypes.Value{"provider_id": str("prov-1")},
		create: create,

		wantSent:    map[string]any{"versionNumber": "1.2.3", "protocols": "5.0,6.0"},
		wantCreated: map[string]string{"version_number": "1.2.3", "protocols": "5.0,6.0", "status": "active", "provider_id": "prov-1", "deprecation_message": "<null>"},
		drift:       map[string]any{"protocols": "6.0"},
		wantDrifted: map[string]string{"protocols": "6.0", "version_number": "1.2.3"},
		update: map[string]tftypes.Value{
			"status":              str("deprecated"),
			"deprecation_message": str("Use 1.3.0, it fixes the import"),
		},
		wantSentPatch: map[string]any{"status": "deprecated", "deprecationMessage": "Use 1.3.0, it fixes the import"},
		wantUpdated:   map[string]string{"status": "deprecated", "deprecation_message": "Use 1.3.0, it fixes the import"},

		deleteMissingIsOK: true,

		importID:     "org-9,prov-9,item-9",
		wantImported: map[string]string{"organization_id": "org-9", "provider_id": "prov-9", "id": "item-9"},
		badImportIDs: []string{"item-9", "org-9,prov-9", "org-9,,item-9", ",prov-9,item-9", "org-9,prov-9,", "org-9,prov-9,item-9,extra", ""},
	}
}

// Status is optional and computed, so it is unknown in a plan when it is not set.
func versionCreate() map[string]tftypes.Value {
	return map[string]tftypes.Value{
		"version_number": tftypes.NewValue(tftypes.String, "1.2.3"),
		"protocols":      protocolList("5.0", "6.0"),
		"status":         tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
	}
}

func TestRegistryProviderVersionResource(t *testing.T) {
	runCrudResourceTests(t, registryProviderVersionCase(versionCreate()))
}

// Terrakube older than 2.34.0 rejects attributes it does not know, so a version that sets neither
// a status nor a message must not send them. It reads back as active.
func TestRegistryProviderVersionResource_NewAttributesAreOnlySentWhenSet(t *testing.T) {
	h := newCrudHarness(t, registryProviderVersionCase(versionCreate()))
	h.createdState()

	request, _ := h.server.lastRequest(http.MethodPost)
	attrs := bodyAttributes(t, request)
	for _, name := range []string{"status", "deprecationMessage"} {
		if _, sent := attrs[name]; sent {
			t.Errorf("%s must not be sent when it is not set, got %v", name, attrs[name])
		}
	}
}

func TestRegistryProviderVersionResource_ProtocolOrderIsKept(t *testing.T) {
	create := versionCreate()
	create["protocols"] = protocolList("6.0", "5.0")
	h := newCrudHarness(t, registryProviderVersionCase(create))
	state := h.createdState()

	request, _ := h.server.lastRequest(http.MethodPost)
	if got := bodyAttributes(t, request)["protocols"]; got != "6.0,5.0" {
		t.Errorf("protocols sent = %v, want 6.0,5.0", got)
	}
	h.checkState(state, map[string]string{"protocols": "6.0,5.0"})
}

// A PATCH that leaves the message out keeps the old one, so removing it has to be sent.
func TestRegistryProviderVersionResource_UpdateClearsRemovedMessage(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	create := versionCreate()
	create["status"] = str("deprecated")
	create["deprecation_message"] = str("Use 1.3.0")
	h := newCrudHarness(t, registryProviderVersionCase(create))
	prior := h.createdState()

	merged := versionCreate()
	merged["status"] = str("active")
	plan := h.value(h.known(), merged)
	resp := &resource.UpdateResponse{State: prior}
	h.res.Update(h.ctx, resource.UpdateRequest{
		State:  prior,
		Plan:   tfsdk.Plan{Schema: h.schema.Schema, Raw: plan},
		Config: tfsdk.Config{Schema: h.schema.Schema, Raw: plan},
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update returned diagnostics: %v", resp.Diagnostics)
	}

	request, _ := h.server.lastRequest(http.MethodPatch)
	if got := bodyAttributes(t, request)["deprecationMessage"]; got != "" {
		t.Errorf("the removed message must be sent empty, got %v", got)
	}
	h.checkState(resp.State, map[string]string{"status": "active", "deprecation_message": "<null>"})
}

// A PATCH of a version that never set a status must not send one, or Terrakube older than 2.34.0
// rejects the unknown attribute.
func TestRegistryProviderVersionResource_UpdateLeavesStatusOutWhenUnchanged(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	h := newCrudHarness(t, registryProviderVersionCase(versionCreate()))
	prior := h.createdState()

	merged := versionCreate()
	merged["protocols"] = protocolList("6.0")
	merged["status"] = str("active")
	plan := h.value(h.known(), merged)
	resp := &resource.UpdateResponse{State: prior}
	h.res.Update(h.ctx, resource.UpdateRequest{
		State:  prior,
		Plan:   tfsdk.Plan{Schema: h.schema.Schema, Raw: plan},
		Config: tfsdk.Config{Schema: h.schema.Schema, Raw: plan},
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update returned diagnostics: %v", resp.Diagnostics)
	}

	request, _ := h.server.lastRequest(http.MethodPatch)
	attrs := bodyAttributes(t, request)
	if _, sent := attrs["status"]; sent {
		t.Errorf("status must not be sent when it is unchanged, got %v", attrs["status"])
	}
	if attrs["protocols"] != "6.0" {
		t.Errorf("protocols sent = %v, want 6.0", attrs["protocols"])
	}
}

// Removing status from the configuration plans active, so deleting `status = "deprecated"` undoes it.
func TestRegistryProviderVersionResource_RemovingStatusPlansActive(t *testing.T) {
	ctx := context.Background()
	modifier := statusDefaultsToActive{}
	stateWith := func(v string) tfsdk.State {
		return tfsdk.State{Raw: tftypes.NewValue(tftypes.String, v)}
	}
	cases := map[string]struct {
		state  tfsdk.State
		config types.String
		want   types.String
	}{
		"removed from a deprecated version": {stateWith("x"), types.StringNull(), types.StringValue("active")},
		"set in the configuration":          {stateWith("x"), types.StringValue("deprecated"), types.StringUnknown()},
		"create":                            {tfsdk.State{Raw: tftypes.NewValue(tftypes.String, nil)}, types.StringNull(), types.StringUnknown()},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := planmodifier.StringRequest{State: tc.state, ConfigValue: tc.config, PlanValue: types.StringUnknown()}
			resp := &planmodifier.StringResponse{PlanValue: types.StringUnknown()}
			modifier.PlanModifyString(ctx, req, resp)
			if !resp.PlanValue.Equal(tc.want) {
				t.Errorf("plan = %v, want %v", resp.PlanValue, tc.want)
			}
		})
	}
}

func TestRegistryProviderVersionResource_Validators(t *testing.T) {
	ctx := context.Background()
	res := &RegistryProviderVersionResource{}
	var schemaResp resource.SchemaResponse
	res.Schema(ctx, resource.SchemaRequest{}, &schemaResp)

	run := func(t *testing.T, value types.String, validators []validator.String) bool {
		t.Helper()
		failed := false
		for _, v := range validators {
			resp := &validator.StringResponse{}
			v.ValidateString(ctx, validator.StringRequest{ConfigValue: value}, resp)
			failed = failed || resp.Diagnostics.HasError()
		}
		return failed
	}

	t.Run("status", func(t *testing.T) {
		attribute, ok := schemaResp.Schema.Attributes["status"].(schema.StringAttribute)
		if !ok {
			t.Fatal("status is not a string attribute")
		}
		for _, status := range []string{"active", "deprecated", "removed"} {
			if run(t, types.StringValue(status), attribute.Validators) {
				t.Errorf("%q should be accepted", status)
			}
		}
		for _, status := range []string{"", "Active", "gone", "deprecated "} {
			if !run(t, types.StringValue(status), attribute.Validators) {
				t.Errorf("%q should be rejected", status)
			}
		}
	})

	t.Run("deprecation_message", func(t *testing.T) {
		attribute, ok := schemaResp.Schema.Attributes["deprecation_message"].(schema.StringAttribute)
		if !ok {
			t.Fatal("deprecation_message is not a string attribute")
		}
		valid := []string{"Use 1.3.0", "Removed on 2027-01-01, see https://example.com/upgrade", "Utilisez la 1.3.0 — corrigé", strings.Repeat("a", 1024)}
		for _, message := range valid {
			if run(t, types.StringValue(message), attribute.Validators) {
				t.Errorf("%q should be accepted", message)
			}
		}
		invalid := map[string]string{
			"empty":                  "",
			"too long":               strings.Repeat("a", 1025),
			"too long in UTF-16":     strings.Repeat("😀", 600),
			"leading space":          " Use 1.3.0",
			"trailing space":         "Use 1.3.0 ",
			"a line break":           "Use 1.3.0\nnow",
			"a tab":                  "Use\t1.3.0",
			"an escape character":    "Use \x1b[31m1.3.0",
			"a bidi override":        "Use \u202e0.3.1",
			"a zero width character": "Use\u200b1.3.0",
		}
		for name, message := range invalid {
			if !run(t, types.StringValue(message), attribute.Validators) {
				t.Errorf("%s should be rejected", name)
			}
		}
		for _, v := range []types.String{types.StringNull(), types.StringUnknown()} {
			if run(t, v, attribute.Validators) {
				t.Errorf("%v should not be validated", v)
			}
		}
	})

	t.Run("protocols", func(t *testing.T) {
		h := newCrudHarness(t, registryProviderVersionCase(versionCreate()))
		attribute, ok := h.schema.Schema.Attributes["protocols"].(schema.ListAttribute)
		if !ok {
			t.Fatal("protocols is not a list attribute")
		}
		check := func(values ...string) bool {
			elements := make([]types.String, len(values))
			list := make([]attrValue, len(values))
			for i, v := range values {
				elements[i] = types.StringValue(v)
				list[i] = elements[i]
			}
			failed := false
			for _, v := range attribute.Validators {
				resp := &validator.ListResponse{}
				v.ValidateList(ctx, validator.ListRequest{ConfigValue: types.ListValueMust(types.StringType, list)}, resp)
				failed = failed || resp.Diagnostics.HasError()
			}
			return failed
		}
		for _, ok := range [][]string{{"5.0"}, {"6.0"}, {"5.0", "6.0"}, {"5.10"}} {
			if check(ok...) {
				t.Errorf("%v should be accepted", ok)
			}
		}
		for _, bad := range [][]string{{}, {""}, {"6"}, {"v6.0"}, {"5.0,6.0"}, {"6.0.1"}, {"six"}} {
			if !check(bad...) {
				t.Errorf("%v should be rejected", bad)
			}
		}
	})
}
