package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func sshResourceSchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	r := &SshResource{}
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

func TestSshResource_Schema(t *testing.T) {
	ctx := context.Background()
	s, _ := sshResourceSchemaAndType(t, ctx)

	// organization_id must be required
	if !s.Attributes["organization_id"].IsRequired() {
		t.Errorf("expected organization_id to be required")
	}

	// private_key must be required and sensitive
	privKeyAttr := s.Attributes["private_key"]
	if !privKeyAttr.IsRequired() {
		t.Errorf("expected private_key to be required")
	}
	if !privKeyAttr.IsSensitive() {
		t.Errorf("expected private_key to be sensitive")
	}

	// id must be computed
	if !s.Attributes["id"].IsComputed() {
		t.Errorf("expected id to be computed")
	}

	// ssh_type should be optional and computed
	if !s.Attributes["ssh_type"].IsOptional() || !s.Attributes["ssh_type"].IsComputed() {
		t.Errorf("expected ssh_type to be optional and computed")
	}
}

func TestSshResource_Create_Success(t *testing.T) {
	ctx := context.Background()
	s, objType := sshResourceSchemaAndType(t, ctx)

	const orgID = "org-test-1"

	var capturedBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization/"+orgID+"/ssh", func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		capturedBody = body
		w.Header().Set("Content-Type", "application/vnd.api+json")
		fmt.Fprint(w, `{"data":{"type":"ssh","id":"ssh-123","attributes":{`+
			`"name":"deploy-key","description":"test description",`+
			`"sshType":"rsa"}}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &SshResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	planValue := buildObjectValue(objType, map[string]tftypes.Value{
		"organization_id": tftypes.NewValue(tftypes.String, orgID),
		"name":            tftypes.NewValue(tftypes.String, "deploy-key"),
		"description":     tftypes.NewValue(tftypes.String, "test description"),
		"private_key":     tftypes.NewValue(tftypes.String, "-----BEGIN OPENSSH PRIVATE KEY-----\ntest\n-----END OPENSSH PRIVATE KEY-----"),
		"ssh_type":        tftypes.NewValue(tftypes.String, "rsa"),
	})

	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValue}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

	r.Create(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics)
	}

	if !strings.Contains(string(capturedBody), "BEGIN OPENSSH PRIVATE KEY") {
		t.Errorf("expected request body to contain privateKey, got: %s", capturedBody)
	}
	if !strings.Contains(string(capturedBody), "deploy-key") {
		t.Errorf("expected request body to contain name, got: %s", capturedBody)
	}

	var result SshResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}

	if result.ID.ValueString() != "ssh-123" {
		t.Errorf("expected ID ssh-123, got: %v", result.ID.ValueString())
	}
	if result.PrivateKey.ValueString() != "-----BEGIN OPENSSH PRIVATE KEY-----\ntest\n-----END OPENSSH PRIVATE KEY-----" {
		t.Errorf("expected private_key preserved in state, got: %v", result.PrivateKey.ValueString())
	}
}

func TestSshResource_ImportState(t *testing.T) {
	ctx := context.Background()
	s, objType := sshResourceSchemaAndType(t, ctx)

	r := &SshResource{}

	// Valid import ID
	req := resource.ImportStateRequest{ID: "org-1,ssh-1"}
	resp := &resource.ImportStateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}

	r.ImportState(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var orgID, sshID string
	resp.State.GetAttribute(ctx, path.Root("organization_id"), &orgID)
	resp.State.GetAttribute(ctx, path.Root("id"), &sshID)

	if orgID != "org-1" || sshID != "ssh-1" {
		t.Errorf("expected org-1, ssh-1; got %s, %s", orgID, sshID)
	}

	// Invalid import ID
	invalidReq := resource.ImportStateRequest{ID: "invalid-id-without-comma"}
	invalidResp := &resource.ImportStateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}

	r.ImportState(ctx, invalidReq, invalidResp)

	if !invalidResp.Diagnostics.HasError() {
		t.Errorf("expected diagnostics error for invalid import ID format")
	}
}
