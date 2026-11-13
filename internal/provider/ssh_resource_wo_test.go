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
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const sshTestKey = "-----BEGIN OPENSSH PRIVATE KEY-----wo-test"

// sshServer answers as Terrakube does: privateKey is never in a response. It
// records the body of the last POST or PATCH.
func sshServer(t *testing.T, captured *[]byte) *httptest.Server {
	t.Helper()

	base := "/api/v1/organization/org-1/ssh"
	response := `{"data":{"type":"ssh","id":"ssh-1","attributes":{"name":"deploy-key","description":"desc","sshType":"rsa"}}}`

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

	mux := http.NewServeMux()
	mux.HandleFunc(base, handler)
	mux.HandleFunc(base+"/ssh-1", handler)
	return httptest.NewServer(mux)
}

func sshValues(objType tftypes.Object, overrides map[string]tftypes.Value) tftypes.Value {
	values := map[string]tftypes.Value{
		"organization_id": tftypes.NewValue(tftypes.String, "org-1"),
		"name":            tftypes.NewValue(tftypes.String, "deploy-key"),
		"description":     tftypes.NewValue(tftypes.String, "desc"),
		"ssh_type":        tftypes.NewValue(tftypes.String, "rsa"),
	}
	maps.Copy(values, overrides)
	return buildObjectValue(objType, values)
}

func TestSshResource_Create_PrivateKeyWo(t *testing.T) {
	ctx := context.Background()
	s, objType := sshResourceSchemaAndType(t, ctx)

	var captured []byte
	server := sshServer(t, &captured)
	defer server.Close()

	r := &SshResource{client: server.Client(), endpoint: server.URL, token: "test-token"}

	// Write-only values are null in the plan; only the configuration has them.
	planValue := sshValues(objType, map[string]tftypes.Value{
		"private_key_wo_version": tftypes.NewValue(tftypes.Number, 1),
	})
	configValue := sshValues(objType, map[string]tftypes.Value{
		"private_key_wo":         tftypes.NewValue(tftypes.String, sshTestKey),
		"private_key_wo_version": tftypes.NewValue(tftypes.Number, 1),
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
	if !strings.Contains(string(captured), sshTestKey) {
		t.Errorf("expected the request body to carry private_key_wo, got: %s", captured)
	}

	var result SshResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}
	// Null, not "": an empty string would not match the null plan value.
	if !result.PrivateKey.IsNull() || !result.PrivateKeyWo.IsNull() {
		t.Errorf("expected private_key and private_key_wo to stay null in state, got %v / %v", result.PrivateKey, result.PrivateKeyWo)
	}
	if result.PrivateKeyWoVersion.ValueInt64() != 1 {
		t.Errorf("expected private_key_wo_version 1 in state, got: %d", result.PrivateKeyWoVersion.ValueInt64())
	}
}

func sshUpdate(t *testing.T, ctx context.Context, s schema.Schema, state, plan, config tftypes.Value) ([]byte, SshResourceModel) {
	t.Helper()

	var captured []byte
	server := sshServer(t, &captured)
	defer server.Close()

	r := &SshResource{client: server.Client(), endpoint: server.URL, token: "test-token"}

	req := resource.UpdateRequest{
		State:  tfsdk.State{Schema: s, Raw: state},
		Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		Config: tfsdk.Config{Schema: s, Raw: config},
	}
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s}}

	r.Update(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Update returned diagnostics: %v", resp.Diagnostics)
	}

	var result SshResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}
	return captured, result
}

// An update that does not touch the key must still send it: the PATCH carries
// every attribute, and an empty private key would clear it.
func TestSshResource_Update_KeepsPrivateKeyWo(t *testing.T) {
	ctx := context.Background()
	s, objType := sshResourceSchemaAndType(t, ctx)

	id := tftypes.NewValue(tftypes.String, "ssh-1")
	version := tftypes.NewValue(tftypes.Number, 1)

	state := sshValues(objType, map[string]tftypes.Value{"id": id, "private_key_wo_version": version})
	plan := sshValues(objType, map[string]tftypes.Value{
		"id":                     id,
		"description":            tftypes.NewValue(tftypes.String, "new description"),
		"private_key_wo_version": version,
	})
	config := sshValues(objType, map[string]tftypes.Value{
		"description":            tftypes.NewValue(tftypes.String, "new description"),
		"private_key_wo":         tftypes.NewValue(tftypes.String, sshTestKey),
		"private_key_wo_version": version,
	})

	captured, result := sshUpdate(t, ctx, s, state, plan, config)

	if !strings.Contains(string(captured), sshTestKey) {
		t.Errorf("expected the PATCH body to carry private_key_wo, got: %s", captured)
	}
	if !result.PrivateKey.IsNull() || !result.PrivateKeyWo.IsNull() {
		t.Errorf("expected private_key and private_key_wo to stay null in state")
	}
}

// Bumping private_key_wo_version is an in-place update that sends the new key.
func TestSshResource_Update_WoVersionBump(t *testing.T) {
	ctx := context.Background()
	s, objType := sshResourceSchemaAndType(t, ctx)

	version, ok := s.Attributes["private_key_wo_version"].(schema.Int64Attribute)
	if !ok {
		t.Fatalf("expected private_key_wo_version to be an Int64Attribute")
	}
	if len(version.PlanModifiers) != 0 {
		t.Errorf("expected private_key_wo_version to have no plan modifiers (a bump must not replace)")
	}

	id := tftypes.NewValue(tftypes.String, "ssh-1")
	state := sshValues(objType, map[string]tftypes.Value{"id": id, "private_key_wo_version": tftypes.NewValue(tftypes.Number, 1)})
	plan := sshValues(objType, map[string]tftypes.Value{"id": id, "private_key_wo_version": tftypes.NewValue(tftypes.Number, 2)})
	config := sshValues(objType, map[string]tftypes.Value{
		"private_key_wo":         tftypes.NewValue(tftypes.String, "rotated-key"),
		"private_key_wo_version": tftypes.NewValue(tftypes.Number, 2),
	})

	captured, result := sshUpdate(t, ctx, s, state, plan, config)

	if !strings.Contains(string(captured), "rotated-key") {
		t.Errorf("expected the PATCH body to carry the new private_key_wo, got: %s", captured)
	}
	if result.ID.ValueString() != "ssh-1" {
		t.Errorf("expected ID ssh-1 to be kept, got: %q", result.ID.ValueString())
	}
	if result.PrivateKeyWoVersion.ValueInt64() != 2 {
		t.Errorf("expected private_key_wo_version 2 in state, got: %d", result.PrivateKeyWoVersion.ValueInt64())
	}
}

// Read must keep private_key from the state: null stays null (private_key_wo),
// and a value is kept although Terrakube does not return it.
func TestSshResource_Read_KeepsPrivateKey(t *testing.T) {
	ctx := context.Background()
	s, objType := sshResourceSchemaAndType(t, ctx)

	var captured []byte
	server := sshServer(t, &captured)
	defer server.Close()

	r := &SshResource{client: server.Client(), endpoint: server.URL, token: "test-token"}

	for name, key := range map[string]tftypes.Value{
		"null":  tftypes.NewValue(tftypes.String, nil),
		"value": tftypes.NewValue(tftypes.String, sshTestKey),
	} {
		t.Run(name, func(t *testing.T) {
			stateValue := sshValues(objType, map[string]tftypes.Value{
				"id":          tftypes.NewValue(tftypes.String, "ssh-1"),
				"private_key": key,
			})
			req := resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: stateValue}}
			resp := &resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: stateValue}}

			r.Read(ctx, req, resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics)
			}
			var got types.String
			resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root("private_key"), &got)...)
			if key.IsNull() {
				if !got.IsNull() {
					t.Errorf("expected private_key to stay null, got: %v", got)
				}
				return
			}
			if got.ValueString() != sshTestKey {
				t.Errorf("expected private_key to be kept from the state, got: %v", got)
			}
		})
	}
}

func TestSshResource_ConfigValidators(t *testing.T) {
	ctx := context.Background()
	s, objType := sshResourceSchemaAndType(t, ctx)
	r := &SshResource{}

	cases := map[string]struct {
		overrides map[string]tftypes.Value
		wantError bool
	}{
		"no key": {wantError: true},
		"private_key and private_key_wo": {
			overrides: map[string]tftypes.Value{
				"private_key":    tftypes.NewValue(tftypes.String, "a"),
				"private_key_wo": tftypes.NewValue(tftypes.String, "b"),
			},
			wantError: true,
		},
		"only private_key": {
			overrides: map[string]tftypes.Value{"private_key": tftypes.NewValue(tftypes.String, "a")},
		},
		"only private_key_wo": {
			overrides: map[string]tftypes.Value{"private_key_wo": tftypes.NewValue(tftypes.String, "b")},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: s, Raw: sshValues(objType, tc.overrides)}}

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

func TestSshResource_WoVersionRequiresWo(t *testing.T) {
	ctx := context.Background()
	s, objType := sshResourceSchemaAndType(t, ctx)

	attr, ok := s.Attributes["private_key_wo_version"].(schema.Int64Attribute)
	if !ok {
		t.Fatalf("expected private_key_wo_version to be an Int64Attribute")
	}

	req := validator.Int64Request{
		Path:           path.Root("private_key_wo_version"),
		PathExpression: path.MatchRoot("private_key_wo_version"),
		ConfigValue:    types.Int64Value(1),
		Config: tfsdk.Config{Schema: s, Raw: sshValues(objType, map[string]tftypes.Value{
			"private_key":            tftypes.NewValue(tftypes.String, "plain"),
			"private_key_wo_version": tftypes.NewValue(tftypes.Number, 1),
		})},
	}

	var gotError bool
	for _, v := range attr.Validators {
		resp := &validator.Int64Response{}
		v.ValidateInt64(ctx, req, resp)
		gotError = gotError || resp.Diagnostics.HasError()
	}
	if !gotError {
		t.Errorf("expected private_key_wo_version without private_key_wo to be rejected")
	}
}
