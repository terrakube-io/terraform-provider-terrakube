package provider

import (
	"context"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// registryObjectType returns the object type of a resource's schema.
func registryObjectType(t *testing.T, r resource.Resource) tftypes.Object {
	t.Helper()
	ctx := context.Background()
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	objType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("expected schema type to be a tftypes.Object")
	}
	return objType
}

// Terrakube may answer an attribute that was never set with "". The update keeps the planned null
// instead, or Terraform would report an inconsistent result.
func TestRegistryProviderResource_UpdateKeepsNullForEmptyStrings(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	server, sent := policyTestServer(t, `{"data":{"type":"provider","id":"p-1","attributes":{
		"name":"internal-v2","description":"","imported":false,"registryNamespace":""
	}}}`)
	r := &RegistryProviderResource{client: newTestClient(server.Client(), server.URL)}
	objType := registryObjectType(t, r)
	common := map[string]tftypes.Value{
		"id": str("p-1"), "organization_id": str("org-1"), "imported": tftypes.NewValue(tftypes.Bool, false),
	}
	prior, plan := map[string]tftypes.Value{"name": str("internal")}, map[string]tftypes.Value{"name": str("internal-v2")}
	for k, v := range common {
		prior[k], plan[k] = v, v
	}

	state := runUpdate(t, r, buildObjectValue(objType, prior), buildObjectValue(objType, plan))

	for name, want := range map[string]string{"name": "internal-v2", "description": "<null>", "registry_namespace": "<null>"} {
		if got := policyStateString(t, state, name); got != want {
			t.Errorf("state %s = %q, want %q", name, got, want)
		}
	}
	attributes := sentAttributes(sent(http.MethodPatch))
	if attributes["name"] != "internal-v2" {
		t.Errorf("name sent = %v, want internal-v2", attributes["name"])
	}
	for _, name := range []string{"description", "imported", "registryNamespace"} {
		if value, ok := attributes[name]; ok {
			t.Errorf("%s must not be sent, got %v", name, value)
		}
	}
}

// A Terrakube older than 2.34.0 has no status and may answer an unset message with "". The update
// keeps active and the planned null.
func TestRegistryProviderVersionResource_UpdateKeepsNullForEmptyStrings(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	server, sent := policyTestServer(t, `{"data":{"type":"version","id":"v-1","attributes":{
		"versionNumber":"1.2.3","protocols":"6.0","deprecationMessage":""
	}}}`)
	r := &RegistryProviderVersionResource{client: newTestClient(server.Client(), server.URL)}
	objType := registryObjectType(t, r)
	common := map[string]tftypes.Value{
		"id": str("v-1"), "organization_id": str("org-1"), "provider_id": str("prov-1"),
		"version_number": str("1.2.3"), "status": str("active"),
	}
	prior, plan := map[string]tftypes.Value{"protocols": protocolList("5.0")}, map[string]tftypes.Value{"protocols": protocolList("6.0")}
	for k, v := range common {
		prior[k], plan[k] = v, v
	}

	state := runUpdate(t, r, buildObjectValue(objType, prior), buildObjectValue(objType, plan))

	for name, want := range map[string]string{"status": "active", "deprecation_message": "<null>"} {
		if got := policyStateString(t, state, name); got != want {
			t.Errorf("state %s = %q, want %q", name, got, want)
		}
	}
	attributes := sentAttributes(sent(http.MethodPatch))
	if attributes["protocols"] != "6.0" || attributes["versionNumber"] != "1.2.3" {
		t.Errorf("PATCH sent %v, want versionNumber 1.2.3 and protocols 6.0", attributes)
	}
	for _, name := range []string{"status", "deprecationMessage"} {
		if value, ok := attributes[name]; ok {
			t.Errorf("%s must not be sent, got %v", name, value)
		}
	}
}

// Terrakube stores an unset optional attribute of an implementation as "" (the columns are NOT
// NULL). The update keeps the planned null, and still sends every attribute, with
// ascii_armor as null.
func TestRegistryProviderImplementationResource_UpdateKeepsNullForEmptyStrings(t *testing.T) {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	server, sent := policyTestServer(t, `{"data":{"type":"implementation","id":"i-1","attributes":{
		"os":"linux","arch":"amd64","filename":"provider.zip","downloadUrl":"https://mirror.example.com/provider.zip",
		"shasumsUrl":"","shasumsSignatureUrl":"","shasum":"","keyId":"","asciiArmor":"",
		"trustSignature":"","source":"","sourceUrl":""
	}}}`)
	r := &RegistryProviderImplementationResource{client: newTestClient(server.Client(), server.URL)}
	objType := registryObjectType(t, r)
	common := map[string]tftypes.Value{
		"id": str("i-1"), "organization_id": str("org-1"), "provider_id": str("prov-1"), "version_id": str("ver-1"),
		"os": str("linux"), "arch": str("amd64"), "filename": str("provider.zip"),
	}
	prior := map[string]tftypes.Value{"download_url": str("https://example.com/provider.zip")}
	plan := map[string]tftypes.Value{"download_url": str("https://mirror.example.com/provider.zip")}
	for k, v := range common {
		prior[k], plan[k] = v, v
	}

	state := runUpdate(t, r, buildObjectValue(objType, prior), buildObjectValue(objType, plan))

	if got := policyStateString(t, state, "download_url"); got != "https://mirror.example.com/provider.zip" {
		t.Errorf("state download_url = %q", got)
	}
	for _, name := range []string{"shasums_url", "shasums_signature_url", "shasum", "key_id", "ascii_armor", "trust_signature", "source", "source_url"} {
		if got := policyStateString(t, state, name); got != "<null>" {
			t.Errorf("state %s = %q, want <null>", name, got)
		}
	}
	attributes := sentAttributes(sent(http.MethodPatch))
	for _, name := range []string{"shasumsUrl", "shasumsSignatureUrl", "shasum", "keyId", "trustSignature", "source", "sourceUrl"} {
		if value, ok := attributes[name]; !ok || value != "" {
			t.Errorf("%s must be sent as an empty string, got %v (sent: %v)", name, value, ok)
		}
	}
	if value, ok := attributes["asciiArmor"]; !ok || value != nil {
		t.Errorf("asciiArmor must be sent as null, got %v (sent: %v)", value, ok)
	}
	if attributes["os"] != "linux" || attributes["arch"] != "amd64" || attributes["filename"] != "provider.zip" {
		t.Errorf("PATCH sent %v, want os, arch and filename", attributes)
	}
}
