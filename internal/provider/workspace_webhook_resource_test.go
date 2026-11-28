package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// TestWorkspaceWebhookResource_Read refreshes id and remote_hook_id from the API and keeps the
// attributes terrakube-go's Webhook does not carry.
func TestWorkspaceWebhookResource_Read(t *testing.T) {
	ctx := context.Background()

	r, ok := NewWorkspaceWebhookResource().(*WorkspaceWebhookResource)
	if !ok {
		t.Fatalf("NewWorkspaceWebhookResource returned %T", NewWorkspaceWebhookResource())
	}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	objType, _ := s.Type().TerraformType(ctx).(tftypes.Object)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet || req.URL.Path != "/api/v1/organization/org-1/workspace/ws-1/webhook/wh-1" {
			t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
		}
		fmt.Fprint(w, `{"data":{"type":"webhook","id":"wh-1","attributes":{"remoteHookId":"rh-2"}}}`)
	}))
	defer server.Close()
	r.client = newTestClient(server.Client(), server.URL)

	stateValue := buildObjectValue(objType, map[string]tftypes.Value{
		"id":              tftypes.NewValue(tftypes.String, "wh-1"),
		"organization_id": tftypes.NewValue(tftypes.String, "org-1"),
		"workspace_id":    tftypes.NewValue(tftypes.String, "ws-1"),
		"path":            tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{tftypes.NewValue(tftypes.String, "src/.*")}),
		"remote_hook_id":  tftypes.NewValue(tftypes.String, "rh-1"),
		"event":           tftypes.NewValue(tftypes.String, "PUSH"),
	})
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: stateValue}}
	r.Read(ctx, resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: stateValue}}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics)
	}

	var result WorkspaceWebhookResourceModel
	resp.Diagnostics.Append(resp.State.Get(ctx, &result)...)
	var paths []string
	result.Path.ElementsAs(ctx, &paths, false)
	if result.RemoteHookId.ValueString() != "rh-2" {
		t.Errorf("remote_hook_id = %q, want rh-2 from the API", result.RemoteHookId.ValueString())
	}
	if len(paths) != 1 || paths[0] != "src/.*" || result.Event.ValueString() != "PUSH" {
		t.Errorf("expected path and event kept from state, got %v and %q", paths, result.Event.ValueString())
	}
}
