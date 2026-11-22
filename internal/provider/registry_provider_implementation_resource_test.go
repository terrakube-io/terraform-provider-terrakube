package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func registryProviderImplementationCase(create map[string]tftypes.Value) crudResourceCase {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	return crudResourceCase{
		itemType:   "implementation",
		collection: "/api/v1/organization/org-1/provider/prov-1/version/ver-1/implementation",
		newSource: func(c *http.Client, endpoint string) resource.Resource {
			return &RegistryProviderImplementationResource{client: newTestClient(c, endpoint)}
		},
		scope:  map[string]tftypes.Value{"provider_id": str("prov-1"), "version_id": str("ver-1")},
		create: create,

		wantSent: map[string]any{
			"os": "linux", "arch": "amd64", "filename": "terraform-provider-internal_1.2.3_linux_amd64.zip",
			"downloadUrl": "https://example.com/internal_1.2.3_linux_amd64.zip", "shasum": "abc123",
		},
		wantCreated: map[string]string{
			"os": "linux", "arch": "amd64", "download_url": "https://example.com/internal_1.2.3_linux_amd64.zip",
			"shasum": "abc123", "provider_id": "prov-1", "version_id": "ver-1", "key_id": "<null>",
		},
		drift:         map[string]any{"shasum": "def456"},
		wantDrifted:   map[string]string{"shasum": "def456", "os": "linux"},
		update:        map[string]tftypes.Value{"download_url": str("https://mirror.example.com/internal_1.2.3_linux_amd64.zip")},
		wantSentPatch: map[string]any{"downloadUrl": "https://mirror.example.com/internal_1.2.3_linux_amd64.zip"},
		wantUpdated:   map[string]string{"download_url": "https://mirror.example.com/internal_1.2.3_linux_amd64.zip"},

		deleteMissingIsOK: true,

		importID:     "org-9,prov-9,ver-9,item-9",
		wantImported: map[string]string{"organization_id": "org-9", "provider_id": "prov-9", "version_id": "ver-9", "id": "item-9"},
		badImportIDs: []string{"item-9", "org-9,prov-9,ver-9", "org-9,prov-9,,item-9", ",prov-9,ver-9,item-9", "org-9,prov-9,ver-9,", "org-9,prov-9,ver-9,item-9,extra", ""},
	}
}

func implementationCreate() map[string]tftypes.Value {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	return map[string]tftypes.Value{
		"os":           str("linux"),
		"arch":         str("amd64"),
		"filename":     str("terraform-provider-internal_1.2.3_linux_amd64.zip"),
		"download_url": str("https://example.com/internal_1.2.3_linux_amd64.zip"),
		"shasum":       str("abc123"),
	}
}

func TestRegistryProviderImplementationResource(t *testing.T) {
	runCrudResourceTests(t, registryProviderImplementationCase(implementationCreate()))
}

// An attribute removed from the configuration is sent as an empty string, which clears it. Leaving
// it out of a PATCH would keep the old value. It is not sent as null, because the column is NOT NULL.
func TestRegistryProviderImplementationResource_RemovingAnOptionalAttributeClearsIt(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	const armor = "-----BEGIN PGP PUBLIC KEY BLOCK-----\n\nmQINBF\n-----END PGP PUBLIC KEY BLOCK-----"
	create := implementationCreate()
	create["key_id"] = str("34365D9472D7468F")
	create["ascii_armor"] = str(armor)
	tc := registryProviderImplementationCase(create)
	tc.update = map[string]tftypes.Value{"key_id": tftypes.NewValue(tftypes.String, nil)}
	tc.wantSentPatch = map[string]any{"keyId": ""}
	tc.wantUpdated = map[string]string{"key_id": "<null>", "ascii_armor": armor}
	tc.wantCreated = map[string]string{"key_id": "34365D9472D7468F"}
	tc.drift = map[string]any{}
	tc.wantDrifted = map[string]string{}

	h := newCrudHarness(t, tc)
	prior := h.createdState()
	resp := h.update(prior)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update returned diagnostics: %v", resp.Diagnostics)
	}
	request, _ := h.server.lastRequest(http.MethodPatch)
	attrs := bodyAttributes(t, request)
	if value, sent := attrs["keyId"]; !sent || value != "" {
		t.Errorf("keyId must be sent as an empty string to clear it, got %v (sent: %v)", value, sent)
	}
	h.checkState(resp.State, map[string]string{"key_id": "<null>", "shasum": "abc123"})
}

// Only os, arch, filename and download_url are required. The others read back as null. Terrakube's
// columns for them are NOT NULL, so the create body sends an empty string for each, except
// ascii_armor, which is nullable.
func TestRegistryProviderImplementationResource_MinimalImplementation(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	h := newCrudHarness(t, registryProviderImplementationCase(map[string]tftypes.Value{
		"os": str("darwin"), "arch": str("arm64"), "filename": str("provider.zip"), "download_url": str("https://example.com/provider.zip"),
	}))
	state := h.createdState()
	request, _ := h.server.lastRequest(http.MethodPost)
	attrs := bodyAttributes(t, request)
	for _, key := range []string{"shasumsUrl", "shasumsSignatureUrl", "shasum", "keyId", "trustSignature", "source", "sourceUrl"} {
		if value, sent := attrs[key]; !sent || value != "" {
			t.Errorf("%s must be sent as an empty string when unset, got %v (sent: %v)", key, value, sent)
		}
	}
	if value, sent := attrs["asciiArmor"]; sent && value != nil {
		t.Errorf("asciiArmor is nullable and must not be sent when unset, got %v", value)
	}
	h.checkState(state, map[string]string{
		"os": "darwin", "arch": "arm64", "shasum": "<null>", "shasums_url": "<null>", "shasums_signature_url": "<null>",
		"key_id": "<null>", "ascii_armor": "<null>", "trust_signature": "<null>", "source": "<null>", "source_url": "<null>",
	})
}
