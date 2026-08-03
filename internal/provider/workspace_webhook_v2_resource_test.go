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

func workspaceWebhookV2SchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	r := &WorkspaceWebhookV2Resource{}
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

// TestWorkspaceWebhookV2Resource_Create_ResolvesMigratedV2WhenUnset covers a
// real bug: migrated_v2 is Optional+Computed with no Default, so Terraform
// leaves it unknown (not null) during Create when it's absent from config
// (exactly how main.tf's `migrated_v2 = true` line was learned to be
// necessary — without it the shared-webhook path never activates server
// side). Create() never wrote the value it actually sent back into state,
// so Terraform's protocol rejected the result as still-unknown-after-apply.
func TestWorkspaceWebhookV2Resource_Create_ResolvesMigratedV2WhenUnset(t *testing.T) {
	ctx := context.Background()
	s, objType := workspaceWebhookV2SchemaAndType(t, ctx)

	const orgID = "org-1"
	const wsID = "ws-1"

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/operations", func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		if strings.Contains(string(body), `"migratedV2":true`) {
			t.Errorf("expected migratedV2:false to be sent when unset in config, got: %s", body)
		}
		fmt.Fprint(w, `{"atomic:results":[{"data":{"type":"webhook","id":"wh-created-1"}}]}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &WorkspaceWebhookV2Resource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	planValue := buildObjectValue(objType, map[string]tftypes.Value{
		"organization_id": tftypes.NewValue(tftypes.String, orgID),
		"workspace_id":    tftypes.NewValue(tftypes.String, wsID),
		// migrated_v2 intentionally left unknown, matching what Terraform
		// actually produces for an Optional+Computed attribute that's
		// absent from config during Create (never null).
		"migrated_v2": tftypes.NewValue(tftypes.Bool, tftypes.UnknownValue),
	})

	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValue}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

	r.Create(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics)
	}

	var result WorkspaceWebhookV2ResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}

	if result.MigratedV2.IsUnknown() {
		t.Fatal("expected migrated_v2 to be resolved to a known value after create, but it is still unknown (Terraform's protocol rejects unknown values after apply)")
	}
	if result.MigratedV2.ValueBool() {
		t.Errorf("expected migrated_v2 to resolve to false (the value actually sent), got true")
	}
}

// TestWorkspaceWebhookV2Resource_Update_SendsValidJsonApiPayload covers a
// real bug: Update() built its PATCH body with plain json.Marshal() on a
// struct that only carries `jsonapi` tags, producing a flat, non-JSON:API
// body (e.g. {"ID":"...","MigratedV2":true}) instead of the required
// {"data":{"type":"webhook","attributes":{...}}} shape. Elide rejected it
// with "Expected data but found null".
func TestWorkspaceWebhookV2Resource_Update_SendsValidJsonApiPayload(t *testing.T) {
	ctx := context.Background()
	s, objType := workspaceWebhookV2SchemaAndType(t, ctx)

	const (
		orgID = "org-1"
		wsID  = "ws-1"
		id    = "wh-1"
	)

	var patchBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization/"+orgID+"/workspace/"+wsID+"/webhook/"+id, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPatch {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("reading PATCH body: %v", err)
			}
			patchBody = string(body)
			fmt.Fprint(w, `{"data":{"type":"webhook","id":"wh-1","attributes":{"migratedV2":true}}}`)
			return
		}
		// Update()'s follow-up GET.
		fmt.Fprint(w, `{"data":{"type":"webhook","id":"wh-1","attributes":{"remoteHookId":"","migratedV2":true}}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &WorkspaceWebhookV2Resource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	base := map[string]tftypes.Value{
		"id":              tftypes.NewValue(tftypes.String, id),
		"organization_id": tftypes.NewValue(tftypes.String, orgID),
		"workspace_id":    tftypes.NewValue(tftypes.String, wsID),
		"remote_hook_id":  tftypes.NewValue(tftypes.String, ""),
	}
	stateOverrides := map[string]tftypes.Value{}
	for k, v := range base {
		stateOverrides[k] = v
	}
	stateOverrides["migrated_v2"] = tftypes.NewValue(tftypes.Bool, false)

	planOverrides := map[string]tftypes.Value{}
	for k, v := range base {
		planOverrides[k] = v
	}
	planOverrides["migrated_v2"] = tftypes.NewValue(tftypes.Bool, true)

	req := resource.UpdateRequest{
		State: tfsdk.State{Schema: s, Raw: buildObjectValue(objType, stateOverrides)},
		Plan:  tfsdk.Plan{Schema: s, Raw: buildObjectValue(objType, planOverrides)},
	}
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s}}

	r.Update(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Update returned diagnostics: %v", resp.Diagnostics)
	}

	if !strings.Contains(patchBody, `"data"`) {
		t.Errorf("expected PATCH body to be JSON:API shaped (contain a top-level \"data\" key), got: %s", patchBody)
	}
	if !strings.Contains(patchBody, `"type":"webhook"`) {
		t.Errorf("expected PATCH body to declare type \"webhook\", got: %s", patchBody)
	}
	if strings.Contains(patchBody, `"createdBy":""`) {
		t.Errorf("expected read-only empty fields to be omitted from the PATCH body, got: %s", patchBody)
	}
}
