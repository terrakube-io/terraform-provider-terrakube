package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func organizationNotificationConfigurationSchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	r := &OrganizationNotificationConfigurationResource{}
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

func TestOrganizationNotificationConfigurationResource_Schema_SigningSecretIsSensitive(t *testing.T) {
	ctx := context.Background()
	s, _ := organizationNotificationConfigurationSchemaAndType(t, ctx)

	attr, ok := s.Attributes["signing_secret"]
	if !ok {
		t.Fatalf("expected a signing_secret attribute")
	}
	if !attr.IsSensitive() {
		t.Errorf("expected signing_secret to be marked Sensitive so it never appears in plan output or logs")
	}
}

func TestOrganizationNotificationConfigurationResource_Create_PostsConfigThenEachTrigger(t *testing.T) {
	ctx := context.Background()
	s, objType := organizationNotificationConfigurationSchemaAndType(t, ctx)

	const orgID = "org-1"
	var triggerPosts []string
	var templatesPatchBody string

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization/org-1/notificationConfiguration", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		fmt.Fprint(w, `{"data":{"type":"notification_configuration","id":"cfg-1","attributes":{
			"name":"prod-alerts","channelType":"SLACK","destinationUrl":"https://hooks.slack.com/x","active":true,"messageStyle":"SIMPLE"}}}`)
	})
	mux.HandleFunc("/api/v1/notification_configuration/cfg-1/triggers", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		triggerPosts = append(triggerPosts, string(body))
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("/api/v1/notification_configuration/cfg-1/relationships/templates", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("unexpected method %s on templates relationship", r.Method)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		templatesPatchBody = string(body)
		w.WriteHeader(http.StatusNoContent)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &OrganizationNotificationConfigurationResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	planValue := buildObjectValue(objType, map[string]tftypes.Value{
		"organization_id": tftypes.NewValue(tftypes.String, orgID),
		"name":            tftypes.NewValue(tftypes.String, "prod-alerts"),
		"channel_type":    tftypes.NewValue(tftypes.String, "SLACK"),
		"destination_url": tftypes.NewValue(tftypes.String, "https://hooks.slack.com/x"),
		"active":          tftypes.NewValue(tftypes.Bool, true),
		"message_style":   tftypes.NewValue(tftypes.String, "SIMPLE"),
		"trigger_statuses": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{
			tftypes.NewValue(tftypes.String, "failed"),
			tftypes.NewValue(tftypes.String, "completed"),
		}),
		"template_ids": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{
			tftypes.NewValue(tftypes.String, "tmpl-1"),
		}),
	})

	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValue}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

	r.Create(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	if len(triggerPosts) != 2 {
		t.Fatalf("expected 2 trigger POSTs, got %d: %v", len(triggerPosts), triggerPosts)
	}
	if !contains(templatesPatchBody, `"id":"tmpl-1"`) {
		t.Errorf("expected templates PATCH body to contain tmpl-1, got: %s", templatesPatchBody)
	}

	var result OrganizationNotificationConfigurationResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}
	if result.ID.ValueString() != "cfg-1" {
		t.Errorf("ID = %q, want %q", result.ID.ValueString(), "cfg-1")
	}
	if result.MessageStyle.ValueString() != "SIMPLE" {
		t.Errorf("MessageStyle = %q, want %q", result.MessageStyle.ValueString(), "SIMPLE")
	}
}
