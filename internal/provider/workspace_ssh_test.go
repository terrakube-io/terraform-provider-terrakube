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
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// TestWorkspaceVcsResource_Create_SendsSshRelationship covers the primary
// ask: a workspace can clone its own repository over raw SSH (as an
// alternative to an OAuth-based vcs connection) by setting ssh_id, which
// must be marshaled as the "ssh" relationship and round-tripped back into
// state from the API response.
func TestWorkspaceVcsResource_Create_SendsSshRelationship(t *testing.T) {
	ctx := context.Background()
	s, objType := workspaceVcsSchemaAndType(t, ctx)

	const orgID = "org-1"

	var capturedBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization/"+orgID+"/workspace", func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		capturedBody = body
		fmt.Fprint(w, `{"data":{"type":"workspace","id":"ws-created-1","attributes":{`+
			`"name":"my-workspace","description":null,"source":"git@example.com:org/repo.git",`+
			`"branch":"main","folder":"/","defaultTemplate":"tmpl-1","iacType":"terraform",`+
			`"terraformVersion":"1.12.0","executionMode":"remote","allowRemoteApply":false},`+
			`"relationships":{"project":{"data":null},"ssh":{"data":{"type":"ssh","id":"ssh-1"}}}}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &WorkspaceVcsResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	planValue := buildObjectValue(objType, map[string]tftypes.Value{
		"organization_id":    tftypes.NewValue(tftypes.String, orgID),
		"name":               tftypes.NewValue(tftypes.String, "my-workspace"),
		"repository":         tftypes.NewValue(tftypes.String, "git@example.com:org/repo.git"),
		"template_id":        tftypes.NewValue(tftypes.String, "tmpl-1"),
		"iac_version":        tftypes.NewValue(tftypes.String, "1.12.0"),
		"branch":             tftypes.NewValue(tftypes.String, "main"),
		"folder":             tftypes.NewValue(tftypes.String, "/"),
		"iac_type":           tftypes.NewValue(tftypes.String, "terraform"),
		"execution_mode":     tftypes.NewValue(tftypes.String, "remote"),
		"allow_remote_apply": tftypes.NewValue(tftypes.Bool, false),
		"ssh_id":             tftypes.NewValue(tftypes.String, "ssh-1"),
		"project_id":         tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
	})

	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValue}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

	r.Create(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics)
	}

	if !strings.Contains(string(capturedBody), `"ssh"`) {
		t.Errorf("expected request body to include the ssh relationship, got: %s", capturedBody)
	}
	if !strings.Contains(string(capturedBody), `"id":"ssh-1"`) {
		t.Errorf("expected request body to reference ssh key id ssh-1, got: %s", capturedBody)
	}

	var result WorkspaceVcsResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}

	if result.SshId.ValueString() != "ssh-1" {
		t.Errorf("expected ssh_id to be ssh-1 after create, got: %v", result.SshId)
	}
}

// TestWorkspaceVcsResource_Update_ResolvesSshIdToNullWhenApiReturnsNoSsh
// mirrors the existing project_id null-resolution coverage: if the API
// reports no ssh relationship, SshId must be explicitly null rather than
// left holding a stale prior value (Terraform's protocol rejects unknown
// values after apply, and a stale value would cause permanent drift).
func TestWorkspaceVcsResource_Update_ResolvesSshIdToNullWhenApiReturnsNoSsh(t *testing.T) {
	ctx := context.Background()
	s, objType := workspaceVcsSchemaAndType(t, ctx)

	const (
		orgID = "org-1"
		wsID  = "ws-1"
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization/"+orgID+"/workspace/"+wsID, func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprint(w, `{"data":{"type":"workspace","id":"ws-1","attributes":{`+
			`"name":"my-workspace","description":null,"source":"https://example.com/repo.git",`+
			`"branch":"main","folder":"/","defaultTemplate":"tmpl-1","iacType":"terraform",`+
			`"terraformVersion":"1.12.0","executionMode":"remote","allowRemoteApply":false},`+
			`"relationships":{"project":{"data":null}}}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &WorkspaceVcsResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	base := map[string]tftypes.Value{
		"id":                 tftypes.NewValue(tftypes.String, wsID),
		"organization_id":    tftypes.NewValue(tftypes.String, orgID),
		"name":               tftypes.NewValue(tftypes.String, "my-workspace"),
		"repository":         tftypes.NewValue(tftypes.String, "https://example.com/repo.git"),
		"template_id":        tftypes.NewValue(tftypes.String, "tmpl-1"),
		"iac_version":        tftypes.NewValue(tftypes.String, "1.12.0"),
		"branch":             tftypes.NewValue(tftypes.String, "main"),
		"folder":             tftypes.NewValue(tftypes.String, "/"),
		"iac_type":           tftypes.NewValue(tftypes.String, "terraform"),
		"execution_mode":     tftypes.NewValue(tftypes.String, "remote"),
		"allow_remote_apply": tftypes.NewValue(tftypes.Bool, false),
		"project_id":         tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
	}

	// Prior state had ssh_id set (e.g. the workspace was switched from an
	// SSH-based source to a vcs connection); the new plan drops it.
	stateOverrides := map[string]tftypes.Value{}
	for k, v := range base {
		stateOverrides[k] = v
	}
	stateOverrides["ssh_id"] = tftypes.NewValue(tftypes.String, "old-ssh-key")

	planOverrides := map[string]tftypes.Value{}
	for k, v := range base {
		planOverrides[k] = v
	}
	planOverrides["ssh_id"] = tftypes.NewValue(tftypes.String, nil)

	req := resource.UpdateRequest{
		State: tfsdk.State{Schema: s, Raw: buildObjectValue(objType, stateOverrides)},
		Plan:  tfsdk.Plan{Schema: s, Raw: buildObjectValue(objType, planOverrides)},
	}
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s}}

	r.Update(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Update returned diagnostics: %v", resp.Diagnostics)
	}

	var result WorkspaceVcsResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}

	if !result.SshId.IsNull() {
		t.Errorf("expected ssh_id to be null after update since the API returned no ssh relationship, got: %v", result.SshId)
	}
}

// TestWorkspaceVcsResource_Create_SendsModuleSshKeyAttribute covers the
// second SSH mechanism: moduleSshKey is a plain attribute (not a
// relationship) that controls which org SSH key is used to download
// private Terraform/OpenTofu modules referenced via git-based module
// sources, independent of how the workspace's own repo is cloned.
func TestWorkspaceVcsResource_Create_SendsModuleSshKeyAttribute(t *testing.T) {
	ctx := context.Background()
	s, objType := workspaceVcsSchemaAndType(t, ctx)

	const orgID = "org-1"

	var capturedBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization/"+orgID+"/workspace", func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		capturedBody = body
		fmt.Fprint(w, `{"data":{"type":"workspace","id":"ws-created-1","attributes":{`+
			`"name":"my-workspace","description":null,"source":"https://example.com/repo.git",`+
			`"branch":"main","folder":"/","defaultTemplate":"tmpl-1","iacType":"terraform",`+
			`"terraformVersion":"1.12.0","executionMode":"remote","allowRemoteApply":false,`+
			`"moduleSshKey":"module-ssh-1"},`+
			`"relationships":{"project":{"data":null}}}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &WorkspaceVcsResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	planValue := buildObjectValue(objType, map[string]tftypes.Value{
		"organization_id":    tftypes.NewValue(tftypes.String, orgID),
		"name":               tftypes.NewValue(tftypes.String, "my-workspace"),
		"repository":         tftypes.NewValue(tftypes.String, "https://example.com/repo.git"),
		"template_id":        tftypes.NewValue(tftypes.String, "tmpl-1"),
		"iac_version":        tftypes.NewValue(tftypes.String, "1.12.0"),
		"branch":             tftypes.NewValue(tftypes.String, "main"),
		"folder":             tftypes.NewValue(tftypes.String, "/"),
		"iac_type":           tftypes.NewValue(tftypes.String, "terraform"),
		"execution_mode":     tftypes.NewValue(tftypes.String, "remote"),
		"allow_remote_apply": tftypes.NewValue(tftypes.Bool, false),
		"module_ssh_key":     tftypes.NewValue(tftypes.String, "module-ssh-1"),
		"project_id":         tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
	})

	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValue}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

	r.Create(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics)
	}

	if !strings.Contains(string(capturedBody), `"moduleSshKey":"module-ssh-1"`) {
		t.Errorf("expected request body to include moduleSshKey attribute, got: %s", capturedBody)
	}

	var result WorkspaceVcsResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}

	if result.ModuleSshKey.ValueString() != "module-ssh-1" {
		t.Errorf("expected module_ssh_key to be module-ssh-1 after create, got: %v", result.ModuleSshKey)
	}
}

// TestWorkspaceCliResource_Create_SendsModuleSshKeyAttribute covers the
// same moduleSshKey attribute on CLI-driven workspaces, since private
// module downloads are independent of whether the workspace itself is
// VCS- or CLI-driven.
func TestWorkspaceCliResource_Create_SendsModuleSshKeyAttribute(t *testing.T) {
	ctx := context.Background()

	r := &WorkspaceCliResource{}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics building schema: %v", schemaResp.Diagnostics)
	}
	s := schemaResp.Schema
	objType, ok := s.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("expected schema type to be a tftypes.Object")
	}

	const orgID = "org-1"

	var capturedBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization/"+orgID+"/workspace", func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		capturedBody = body
		fmt.Fprint(w, `{"data":{"type":"workspace","id":"ws-created-1","attributes":{`+
			`"name":"my-workspace","description":null,"iacType":"terraform",`+
			`"terraformVersion":"1.12.0","executionMode":"remote","moduleSshKey":"module-ssh-2"},`+
			`"relationships":{"project":{"data":null}}}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r.client = server.Client()
	r.endpoint = server.URL
	r.token = "test-token"

	planValue := buildObjectValue(objType, map[string]tftypes.Value{
		"organization_id": tftypes.NewValue(tftypes.String, orgID),
		"name":            tftypes.NewValue(tftypes.String, "my-workspace"),
		"iac_version":     tftypes.NewValue(tftypes.String, "1.12.0"),
		"iac_type":        tftypes.NewValue(tftypes.String, "terraform"),
		"execution_mode":  tftypes.NewValue(tftypes.String, "remote"),
		"module_ssh_key":  tftypes.NewValue(tftypes.String, "module-ssh-2"),
		"project_id":      tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
	})

	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValue}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

	r.Create(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics)
	}

	if !strings.Contains(string(capturedBody), `"moduleSshKey":"module-ssh-2"`) {
		t.Errorf("expected request body to include moduleSshKey attribute, got: %s", capturedBody)
	}

	var result WorkspaceCliResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}

	if result.ModuleSshKey.ValueString() != "module-ssh-2" {
		t.Errorf("expected module_ssh_key to be module-ssh-2 after create, got: %v", result.ModuleSshKey)
	}
}

// TestWorkspaceVcsResource_VcsIdAndSshId_AreMutuallyExclusive exercises the
// actual ConflictsWith validators wired onto vcs_id and ssh_id: a workspace
// clones its source either via an OAuth vcs connection or a raw SSH key,
// never both, so configuring both must fail validation.
func TestWorkspaceVcsResource_VcsIdAndSshId_AreMutuallyExclusive(t *testing.T) {
	ctx := context.Background()
	s, objType := workspaceVcsSchemaAndType(t, ctx)

	configValue := buildObjectValue(objType, map[string]tftypes.Value{
		"organization_id": tftypes.NewValue(tftypes.String, "org-1"),
		"name":            tftypes.NewValue(tftypes.String, "my-workspace"),
		"repository":      tftypes.NewValue(tftypes.String, "https://example.com/repo.git"),
		"template_id":     tftypes.NewValue(tftypes.String, "tmpl-1"),
		"iac_version":     tftypes.NewValue(tftypes.String, "1.12.0"),
		"vcs_id":          tftypes.NewValue(tftypes.String, "vcs-1"),
		"ssh_id":          tftypes.NewValue(tftypes.String, "ssh-1"),
	})
	config := tfsdk.Config{Schema: s, Raw: configValue}

	vcsIDAttr, ok := s.Attributes["vcs_id"].(schema.StringAttribute)
	if !ok {
		t.Fatalf("expected vcs_id to be a StringAttribute")
	}
	if len(vcsIDAttr.Validators) == 0 {
		t.Fatalf("expected vcs_id to carry a ConflictsWith validator against ssh_id")
	}

	resp := &validator.StringResponse{}
	vcsIDAttr.Validators[0].ValidateString(ctx, validator.StringRequest{
		Path:           path.Root("vcs_id"),
		PathExpression: path.MatchRoot("vcs_id"),
		Config:         config,
		ConfigValue:    types.StringValue("vcs-1"),
	}, resp)

	if !resp.Diagnostics.HasError() {
		t.Error("expected setting both vcs_id and ssh_id to fail validation, but ConflictsWith reported no error")
	}
}
