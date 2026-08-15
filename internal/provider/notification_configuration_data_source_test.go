package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func notificationConfigurationDataSourceSchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	d := &NotificationConfigurationDataSource{}
	var schemaResp datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics building schema: %v", schemaResp.Diagnostics)
	}

	objType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("expected schema type to be a tftypes.Object")
	}

	return schemaResp.Schema, objType
}

// TestNotificationConfigurationDataSource_Read_DisambiguatesScopeByWorkspaceId covers the reason
// this data source can't just take the first name match: an org can have a workspace-scoped
// config and an org-wide config sharing the same name (e.g. both called "prod-alerts"). Passing
// workspace_id must select the workspace-scoped one, not whichever the API lists first.
func TestNotificationConfigurationDataSource_Read_DisambiguatesScopeByWorkspaceId(t *testing.T) {
	ctx := context.Background()
	s, objType := notificationConfigurationDataSourceSchemaAndType(t, ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization/org-1/notificationConfiguration", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[
			{"type":"notification_configuration","id":"cfg-org-wide","attributes":{"name":"prod-alerts","channelType":"SLACK","destinationUrl":"https://org","active":true,"messageStyle":"DETAILED"}},
			{"type":"notification_configuration","id":"cfg-ws-override","attributes":{"name":"prod-alerts","channelType":"SLACK","destinationUrl":"https://ws","active":true,"messageStyle":"SIMPLE"},
			 "relationships":{"workspace":{"data":{"type":"workspace","id":"ws-1"}}}}
		]}`)
	})
	mux.HandleFunc("/api/v1/notification_configuration/cfg-ws-override/triggers", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"type":"notification_trigger","id":"trig-1","attributes":{"jobStatus":"failed"}}]}`)
	})
	mux.HandleFunc("/api/v1/notification_configuration/cfg-ws-override/templates", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[]}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	d := &NotificationConfigurationDataSource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	configValue := buildObjectValue(objType, map[string]tftypes.Value{
		"organization_id": tftypes.NewValue(tftypes.String, "org-1"),
		"workspace_id":    tftypes.NewValue(tftypes.String, "ws-1"),
		"name":            tftypes.NewValue(tftypes.String, "prod-alerts"),
	})

	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: configValue}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}

	d.Read(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var result NotificationConfigurationDataSourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}
	if result.ID.ValueString() != "cfg-ws-override" {
		t.Errorf("ID = %q, want %q (the workspace-scoped match, not the org-wide one)", result.ID.ValueString(), "cfg-ws-override")
	}
	if result.DestinationUrl.ValueString() != "https://ws" {
		t.Errorf("DestinationUrl = %q, want %q", result.DestinationUrl.ValueString(), "https://ws")
	}
	if result.MessageStyle.ValueString() != "SIMPLE" {
		t.Errorf("MessageStyle = %q, want %q", result.MessageStyle.ValueString(), "SIMPLE")
	}
}

func TestNotificationConfigurationDataSource_Read_ErrorsWhenNotFound(t *testing.T) {
	ctx := context.Background()
	s, objType := notificationConfigurationDataSourceSchemaAndType(t, ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization/org-1/notificationConfiguration", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[]}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	d := &NotificationConfigurationDataSource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	configValue := buildObjectValue(objType, map[string]tftypes.Value{
		"organization_id": tftypes.NewValue(tftypes.String, "org-1"),
		"name":            tftypes.NewValue(tftypes.String, "does-not-exist"),
	})

	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: configValue}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}

	d.Read(ctx, req, resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected an error diagnostic for a not-found lookup, got none")
	}
}
