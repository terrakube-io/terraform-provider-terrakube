package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// workspaceVcsSchemaAndType returns the resource's schema along with the
// tftypes.Object type it maps to, so tests can build raw plan/state values
// without hand-maintaining a duplicate of the attribute list.
func workspaceVcsSchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	r := &WorkspaceVcsResource{}
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

// TestWorkspaceVcsResource_Create_OmitsProjectRelationshipWhenProjectIdUnset
// covers a real bug: project_id is Optional+Computed with no Default, so
// when it's left out of the config, Terraform's plan leaves it *unknown*
// (not null) during Create. The old guard only checked IsNull(), so it sent
// a "project" relationship with an empty id, which the API rejected with a
// 404 that the provider then couldn't unmarshal.
func TestWorkspaceVcsResource_Create_OmitsProjectRelationshipWhenProjectIdUnset(t *testing.T) {
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
		// project_id is intentionally left unknown, matching what Terraform
		// actually produces for an Optional+Computed attribute that's absent
		// from config during Create (never null).
		"project_id": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
	})

	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValue}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

	r.Create(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics)
	}

	if strings.Contains(string(capturedBody), `"project"`) {
		t.Errorf("expected request body to omit the project relationship when project_id is unset, got: %s", capturedBody)
	}

	var result WorkspaceVcsResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}

	if result.ProjectId.IsUnknown() {
		t.Error("expected project_id to be resolved to null after create, but it is still unknown (Terraform's protocol rejects unknown values after apply)")
	}
	if !result.ProjectId.IsNull() {
		t.Errorf("expected project_id to be null after create since the API returned no project, got: %v", result.ProjectId)
	}
}

// TestWorkspaceVcsResource_Update_ResolvesProjectIdToNullWhenApiReturnsNoProject
// covers the same class of bug as the Create test above, but in Update: if
// the API reports no project relationship, ProjectId must be explicitly set
// to null rather than left holding a stale prior value (or unknown), since
// Terraform's protocol rejects unknown values after apply.
func TestWorkspaceVcsResource_Update_ResolvesProjectIdToNullWhenApiReturnsNoProject(t *testing.T) {
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
	}

	// Prior state had a project_id set (e.g. assigned outside Terraform, or
	// by a previous apply); the new plan drops it.
	stateOverrides := map[string]tftypes.Value{}
	for k, v := range base {
		stateOverrides[k] = v
	}
	stateOverrides["project_id"] = tftypes.NewValue(tftypes.String, "old-project")

	planOverrides := map[string]tftypes.Value{}
	for k, v := range base {
		planOverrides[k] = v
	}
	planOverrides["project_id"] = tftypes.NewValue(tftypes.String, tftypes.UnknownValue)

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

	if result.ProjectId.IsUnknown() {
		t.Error("expected project_id to be resolved to null after update, but it is still unknown (Terraform's protocol rejects unknown values after apply)")
	}
	if !result.ProjectId.IsNull() {
		t.Errorf("expected project_id to be null after update since the API returned no project, got: %v", result.ProjectId)
	}
}
