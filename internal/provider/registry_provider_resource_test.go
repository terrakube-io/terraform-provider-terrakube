package provider

import (
	"context"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func registryProviderCase(create map[string]tftypes.Value) crudResourceCase {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	return crudResourceCase{
		itemType:   "provider",
		collection: "/api/v1/organization/org-1/provider",
		newSource: func(c *http.Client, endpoint string) resource.Resource {
			return &RegistryProviderResource{client: c, endpoint: endpoint, token: "test-token"}
		},
		create:        create,
		wantSent:      map[string]any{"name": "aws", "description": "AWS provider", "imported": true, "registryNamespace": "hashicorp"},
		wantCreated:   map[string]string{"name": "aws", "description": "AWS provider", "imported": "true", "registry_namespace": "hashicorp"},
		drift:         map[string]any{"description": "changed outside Terraform"},
		wantDrifted:   map[string]string{"description": "changed outside Terraform", "name": "aws", "registry_namespace": "hashicorp"},
		update:        map[string]tftypes.Value{"description": str("Mirrored AWS provider")},
		wantSentPatch: map[string]any{"description": "Mirrored AWS provider"},
		wantUpdated:   map[string]string{"description": "Mirrored AWS provider", "imported": "true"},

		deleteMissingIsOK: true,
	}
}

func TestRegistryProviderResource(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	runCrudResourceTests(t, registryProviderCase(map[string]tftypes.Value{
		"name":               str("aws"),
		"description":        str("AWS provider"),
		"imported":           tftypes.NewValue(tftypes.Bool, true),
		"registry_namespace": str("hashicorp"),
	}))
}

// A provider you host yourself sets neither imported nor registry_namespace. Terrakube older than
// 2.30.0 rejects attributes it does not know, so they must not be sent, and imported is read back
// as false.
func TestRegistryProviderResource_NotImportedSendsNoNewAttributes(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	tc := registryProviderCase(map[string]tftypes.Value{
		"name":        str("internal"),
		"description": str("Built in house"),
		// Optional and computed, so unset it is unknown in a plan.
		"imported": tftypes.NewValue(tftypes.Bool, tftypes.UnknownValue),
	})
	tc.collection = "/api/v1/organization/org-1/provider"
	h := newCrudHarness(t, tc)

	state := h.createdState()

	request, ok := h.server.lastRequest(http.MethodPost)
	if !ok {
		t.Fatal("no POST was sent")
	}
	attrs := bodyAttributes(t, request)
	for _, name := range []string{"imported", "registryNamespace"} {
		if _, sent := attrs[name]; sent {
			t.Errorf("%s must not be sent when it is not set, got %v", name, attrs[name])
		}
	}
	h.checkState(state, map[string]string{"name": "internal", "imported": "false", "registry_namespace": "<null>"})
}

// Leaving registry_namespace out of a PATCH keeps the old value, so removing it has to be sent.
func TestRegistryProviderResource_UpdateClearsRemovedNamespace(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	tc := registryProviderCase(map[string]tftypes.Value{
		"name":               str("aws"),
		"description":        str("AWS provider"),
		"imported":           tftypes.NewValue(tftypes.Bool, true),
		"registry_namespace": str("hashicorp"),
	})
	h := newCrudHarness(t, tc)
	prior := h.createdState()

	// No longer mirrored: imported false and no namespace.
	plan := h.value(h.known(), map[string]tftypes.Value{
		"name":               str("aws"),
		"description":        str("AWS provider"),
		"imported":           tftypes.NewValue(tftypes.Bool, false),
		"registry_namespace": tftypes.NewValue(tftypes.String, nil),
	})
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
	if attrs["registryNamespace"] != "" {
		t.Errorf("the removed namespace must be sent empty, got %v", attrs["registryNamespace"])
	}
	if attrs["imported"] != false {
		t.Errorf("imported must be sent as false, got %v", attrs["imported"])
	}
	h.checkState(resp.State, map[string]string{"imported": "false", "registry_namespace": "<null>"})
}

func TestRegistryProviderResource_ConfigValidator(t *testing.T) {
	ctx := context.Background()
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	flag := func(v bool) tftypes.Value { return tftypes.NewValue(tftypes.Bool, v) }
	unknownBool := tftypes.NewValue(tftypes.Bool, tftypes.UnknownValue)
	unknownString := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	null := func(t tftypes.Type) tftypes.Value { return tftypes.NewValue(t, nil) }

	tests := []struct {
		name      string
		imported  tftypes.Value
		namespace tftypes.Value
		wantErr   bool
	}{
		{"mirrored from a namespace", flag(true), str("hashicorp"), false},
		{"not imported, no namespace", flag(false), null(tftypes.String), false},
		{"imported left unset, no namespace", null(tftypes.Bool), null(tftypes.String), false},
		{"imported without a namespace", flag(true), null(tftypes.String), true},
		{"a namespace on a provider that is not imported", flag(false), str("hashicorp"), true},
		{"a namespace with imported unset", null(tftypes.Bool), str("hashicorp"), true},
		{"imported is not known yet", unknownBool, null(tftypes.String), false},
		{"the namespace is not known yet", flag(true), unknownString, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newCrudHarness(t, registryProviderCase(nil))
			config := h.value(null(tftypes.String), map[string]tftypes.Value{
				"name": str("aws"), "imported": test.imported, "registry_namespace": test.namespace,
			})

			withValidators, ok := h.res.(resource.ResourceWithConfigValidators)
			if !ok {
				t.Fatal("the resource has no config validators")
			}
			var diagsHasError bool
			for _, v := range withValidators.ConfigValidators(ctx) {
				resp := &resource.ValidateConfigResponse{}
				v.ValidateResource(ctx, resource.ValidateConfigRequest{
					Config: tfsdk.Config{Schema: h.schema.Schema, Raw: config},
				}, resp)
				diagsHasError = diagsHasError || resp.Diagnostics.HasError()
			}
			if diagsHasError != test.wantErr {
				t.Errorf("error = %v, want %v", diagsHasError, test.wantErr)
			}
		})
	}
}
