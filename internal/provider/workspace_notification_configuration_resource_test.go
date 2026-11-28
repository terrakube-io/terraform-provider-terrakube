package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func workspaceNotificationConfigurationSchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	r := &WorkspaceNotificationConfigurationResource{}
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

func TestWorkspaceNotificationConfigurationResource_Create_PostsToWorkspaceScopedPath(t *testing.T) {
	ctx := context.Background()
	s, objType := workspaceNotificationConfigurationSchemaAndType(t, ctx)

	const orgID = "org-1"
	const wsID = "ws-1"
	var triggerPosts []string

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization/org-1/workspace/ws-1/notificationConfiguration", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		fmt.Fprint(w, `{"data":{"type":"notification_configuration","id":"cfg-override","attributes":{
			"name":"prod-alerts-override","channelType":"SLACK","destinationUrl":"https://hooks.slack.com/x","active":true,"messageStyle":"DETAILED"}}}`)
	})
	mux.HandleFunc("/api/v1/notification_configuration/cfg-override/triggers", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		triggerPosts = append(triggerPosts, string(body))
		w.WriteHeader(http.StatusCreated)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &WorkspaceNotificationConfigurationResource{client: newTestClient(server.Client(), server.URL)}

	planValue := buildObjectValue(objType, map[string]tftypes.Value{
		"organization_id": tftypes.NewValue(tftypes.String, orgID),
		"workspace_id":    tftypes.NewValue(tftypes.String, wsID),
		"name":            tftypes.NewValue(tftypes.String, "prod-alerts-override"),
		"channel_type":    tftypes.NewValue(tftypes.String, "SLACK"),
		"destination_url": tftypes.NewValue(tftypes.String, "https://hooks.slack.com/x"),
		"active":          tftypes.NewValue(tftypes.Bool, true),
		"message_style":   tftypes.NewValue(tftypes.String, "DETAILED"),
		"trigger_statuses": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{
			tftypes.NewValue(tftypes.String, "failed"),
		}),
		"template_ids": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{}),
	})

	req := resource.CreateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: planValue},
		Config: tfsdk.Config{Schema: s, Raw: planValue},
	}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

	r.Create(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	if len(triggerPosts) != 1 {
		t.Fatalf("expected 1 trigger POST, got %d: %v", len(triggerPosts), triggerPosts)
	}

	var result WorkspaceNotificationConfigurationResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}
	if result.ID.ValueString() != "cfg-override" {
		t.Errorf("ID = %q, want %q", result.ID.ValueString(), "cfg-override")
	}
}

// TestWorkspaceNotificationConfigurationResource_Update_ReplacesTemplatesWholesale covers that
// Update always sends the full templates set in its PATCH, not a diff against the prior set -
// matching how notification_configuration_shared.go's notificationTemplatesRelationship (and the
// notification system's own UI) manage this to-many relationship.
func TestWorkspaceNotificationConfigurationResource_Update_ReplacesTemplatesWholesale(t *testing.T) {
	ctx := context.Background()
	s, objType := workspaceNotificationConfigurationSchemaAndType(t, ctx)

	var templatesPatchBody []byte

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/notification_configuration/cfg-1", func(w http.ResponseWriter, r *http.Request) {
		// terrakube-go reads the configuration back after a PATCH that has no body, and Update
		// then reads it once more.
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"data":{"type":"notification_configuration","id":"cfg-1","attributes":{
				"name":"prod-alerts-override","channelType":"SLACK","destinationUrl":"https://hooks.slack.com/x","active":true,"messageStyle":"DETAILED"}}}`)
			return
		}
		if r.Method != http.MethodPatch {
			t.Fatalf("unexpected method %s", r.Method)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		templatesPatchBody = body
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/v1/notification_configuration/cfg-1/triggers", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[]}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &WorkspaceNotificationConfigurationResource{client: newTestClient(server.Client(), server.URL)}

	stateValue := buildObjectValue(objType, map[string]tftypes.Value{
		"id":               tftypes.NewValue(tftypes.String, "cfg-1"),
		"organization_id":  tftypes.NewValue(tftypes.String, "org-1"),
		"workspace_id":     tftypes.NewValue(tftypes.String, "ws-1"),
		"name":             tftypes.NewValue(tftypes.String, "prod-alerts-override"),
		"channel_type":     tftypes.NewValue(tftypes.String, "SLACK"),
		"destination_url":  tftypes.NewValue(tftypes.String, "https://hooks.slack.com/x"),
		"active":           tftypes.NewValue(tftypes.Bool, true),
		"message_style":    tftypes.NewValue(tftypes.String, "DETAILED"),
		"trigger_statuses": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{}),
		"template_ids": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{
			tftypes.NewValue(tftypes.String, "tmpl-old"),
		}),
	})
	planValue := buildObjectValue(objType, map[string]tftypes.Value{
		"id":               tftypes.NewValue(tftypes.String, "cfg-1"),
		"organization_id":  tftypes.NewValue(tftypes.String, "org-1"),
		"workspace_id":     tftypes.NewValue(tftypes.String, "ws-1"),
		"name":             tftypes.NewValue(tftypes.String, "prod-alerts-override"),
		"channel_type":     tftypes.NewValue(tftypes.String, "SLACK"),
		"destination_url":  tftypes.NewValue(tftypes.String, "https://hooks.slack.com/x"),
		"active":           tftypes.NewValue(tftypes.Bool, true),
		"message_style":    tftypes.NewValue(tftypes.String, "DETAILED"),
		"trigger_statuses": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{}),
		"template_ids": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{
			tftypes.NewValue(tftypes.String, "tmpl-new"),
		}),
	})

	req := resource.UpdateRequest{
		State:  tfsdk.State{Schema: s, Raw: stateValue},
		Plan:   tfsdk.Plan{Schema: s, Raw: planValue},
		Config: tfsdk.Config{Schema: s, Raw: planValue},
	}
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s}}

	r.Update(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	ids, ok := sentTemplateIDs(t, templatesPatchBody)
	if !ok || !slices.Contains(ids, "tmpl-new") {
		t.Errorf("expected the PATCH body's templates relationship to contain tmpl-new, got: %s", templatesPatchBody)
	}
	if slices.Contains(ids, "tmpl-old") {
		t.Errorf("expected the PATCH body's templates relationship to NOT contain tmpl-old (full replace, not merge), got: %s", templatesPatchBody)
	}
}
