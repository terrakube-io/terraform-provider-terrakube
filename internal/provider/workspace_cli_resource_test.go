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

// workspaceCliSchemaAndType returns the resource's schema along with the
// tftypes.Object type it maps to, so tests can build raw plan/state values
// without hand-maintaining a duplicate of the attribute list.
func workspaceCliSchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	r := &WorkspaceCliResource{}
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

// TestWorkspaceCliResource_Create_IncludesAgentRelationshipWhenAgentIdSet
// verifies that a configured agent_id is sent as the JSON:API "agent"
// to-one relationship and that the id round-trips back into state.
func TestWorkspaceCliResource_Create_IncludesAgentRelationshipWhenAgentIdSet(t *testing.T) {
	ctx := context.Background()
	s, objType := workspaceCliSchemaAndType(t, ctx)

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
			`"name":"my-workspace","description":null,"source":"empty",`+
			`"branch":"remote-content","defaultTemplate":"","iacType":"terraform",`+
			`"terraformVersion":"1.15.8","executionMode":"remote","policyComplianceStatus":"UNKNOWN"},`+
			`"relationships":{"agent":{"data":{"type":"agent","id":"agent-123"}},`+
			`"project":{"data":null}}}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &WorkspaceCliResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	planValue := buildObjectValue(objType, map[string]tftypes.Value{
		"organization_id": tftypes.NewValue(tftypes.String, orgID),
		"name":            tftypes.NewValue(tftypes.String, "my-workspace"),
		"iac_type":        tftypes.NewValue(tftypes.String, "terraform"),
		"iac_version":     tftypes.NewValue(tftypes.String, "1.15.8"),
		"execution_mode":  tftypes.NewValue(tftypes.String, "remote"),
		"agent_id":        tftypes.NewValue(tftypes.String, "agent-123"),
	})

	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValue}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

	r.Create(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics)
	}

	if !strings.Contains(string(capturedBody), `"agent"`) {
		t.Errorf("expected request body to include the agent relationship, got: %s", capturedBody)
	}
	if !strings.Contains(string(capturedBody), `"agent-123"`) {
		t.Errorf("expected request body to include agent id agent-123, got: %s", capturedBody)
	}

	var result WorkspaceCliResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}

	if result.AgentId.IsUnknown() {
		t.Error("expected agent_id to be resolved after create, but it is still unknown (Terraform's protocol rejects unknown values after apply)")
	}
	if result.AgentId.ValueString() != "agent-123" {
		t.Errorf("expected agent_id to be %q after create, got %q", "agent-123", result.AgentId.ValueString())
	}
}

// TestWorkspaceCliResource_Create_OmitsAgentRelationshipWhenAgentIdUnset
// verifies that when agent_id is left unset (unknown during Create, as
// Terraform produces for an Optional+Computed attribute absent from config),
// no "agent" relationship is sent, and state resolves to null.
func TestWorkspaceCliResource_Create_OmitsAgentRelationshipWhenAgentIdUnset(t *testing.T) {
	ctx := context.Background()
	s, objType := workspaceCliSchemaAndType(t, ctx)

	const orgID = "org-1"

	var capturedBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization/"+orgID+"/workspace", func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		capturedBody = body
		fmt.Fprint(w, `{"data":{"type":"workspace","id":"ws-created-2","attributes":{`+
			`"name":"my-workspace","description":null,"source":"empty",`+
			`"branch":"remote-content","defaultTemplate":"","iacType":"terraform",`+
			`"terraformVersion":"1.15.8","executionMode":"remote","policyComplianceStatus":"UNKNOWN"},`+
			`"relationships":{"agent":{"data":null},"project":{"data":null}}}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &WorkspaceCliResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	planValue := buildObjectValue(objType, map[string]tftypes.Value{
		"organization_id": tftypes.NewValue(tftypes.String, orgID),
		"name":            tftypes.NewValue(tftypes.String, "my-workspace"),
		"iac_type":        tftypes.NewValue(tftypes.String, "terraform"),
		"iac_version":     tftypes.NewValue(tftypes.String, "1.15.8"),
		"execution_mode":  tftypes.NewValue(tftypes.String, "remote"),
		// description and module_ssh_key are intentionally left null via
		// buildObjectValue's defaults; ValueStringPointer handles null.
		// project_id is intentionally left unknown, matching what Terraform
		// actually produces for an Optional+Computed attribute that's absent
		// from config during Create (never null).
		"project_id": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		// agent_id is intentionally left unset/unknown.
		"agent_id": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
	})

	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValue}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

	r.Create(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics)
	}

	if strings.Contains(string(capturedBody), `"agent"`) {
		t.Errorf("expected request body to omit the agent relationship when agent_id is unset, got: %s", capturedBody)
	}

	var result WorkspaceCliResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}

	if result.AgentId.IsUnknown() {
		t.Error("expected agent_id to be resolved to null after create, but it is still unknown (Terraform's protocol rejects unknown values after apply)")
	}
	if !result.AgentId.IsNull() {
		t.Errorf("expected agent_id to be null after create since the API returned no agent, got: %v", result.AgentId)
	}
}

// TestWorkspaceCliResource_Update_ResolvesAgentIdToNullWhenApiReturnsNoAgent
// covers the update path: if the API reports no agent relationship, AgentId
// must be explicitly set to null rather than left holding a stale prior value
// (or unknown), since Terraform's protocol rejects unknown values after apply.
func TestWorkspaceCliResource_Update_ResolvesAgentIdToNullWhenApiReturnsNoAgent(t *testing.T) {
	ctx := context.Background()
	s, objType := workspaceCliSchemaAndType(t, ctx)

	const (
		orgID = "org-1"
		wsID  = "ws-1"
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization/"+orgID+"/workspace/"+wsID, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data":{"type":"workspace","id":"ws-1","attributes":{`+
			`"name":"my-workspace","description":null,"source":"empty",`+
			`"branch":"remote-content","defaultTemplate":"","iacType":"terraform",`+
			`"terraformVersion":"1.15.8","executionMode":"remote","policyComplianceStatus":"UNKNOWN"},`+
			`"relationships":{"agent":{"data":null},"project":{"data":null}}}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &WorkspaceCliResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	base := map[string]tftypes.Value{
		"id":              tftypes.NewValue(tftypes.String, wsID),
		"organization_id": tftypes.NewValue(tftypes.String, orgID),
		"name":            tftypes.NewValue(tftypes.String, "my-workspace"),
		"iac_type":        tftypes.NewValue(tftypes.String, "terraform"),
		"iac_version":     tftypes.NewValue(tftypes.String, "1.15.8"),
		"execution_mode":  tftypes.NewValue(tftypes.String, "remote"),
	}

	// Prior state had an agent_id set; the new plan drops it.
	stateOverrides := map[string]tftypes.Value{}
	for k, v := range base {
		stateOverrides[k] = v
	}
	stateOverrides["agent_id"] = tftypes.NewValue(tftypes.String, "old-agent")

	planOverrides := map[string]tftypes.Value{}
	for k, v := range base {
		planOverrides[k] = v
	}
	planOverrides["agent_id"] = tftypes.NewValue(tftypes.String, tftypes.UnknownValue)

	req := resource.UpdateRequest{
		State: tfsdk.State{Schema: s, Raw: buildObjectValue(objType, stateOverrides)},
		Plan:  tfsdk.Plan{Schema: s, Raw: buildObjectValue(objType, planOverrides)},
	}
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s}}

	r.Update(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Update returned diagnostics: %v", resp.Diagnostics)
	}

	var result WorkspaceCliResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}

	if result.AgentId.IsUnknown() {
		t.Error("expected agent_id to be resolved to null after update, but it is still unknown (Terraform's protocol rejects unknown values after apply)")
	}
	if !result.AgentId.IsNull() {
		t.Errorf("expected agent_id to be null after update since the API returned no agent, got: %v", result.AgentId)
	}
}
