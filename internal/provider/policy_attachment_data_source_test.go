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

func policyAttachmentDataSourceSchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	d := &PolicyAttachmentDataSource{}
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

func TestPolicyAttachmentDataSource_ReadById(t *testing.T) {
	ctx := context.Background()
	s, objType := policyAttachmentDataSourceSchemaAndType(t, ctx)

	const policySetID = "ps-100"
	const attachID = "att-1"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/v1/policy_set/%s/attachments/%s", policySetID, attachID), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"data":{"type":"policy_attachment","id":"att-1","relationships":{
			"workspace":{"data":{"type":"workspace","id":"ws-10"}}
		}}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	d := &PolicyAttachmentDataSource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	configValue := buildObjectValue(objType, map[string]tftypes.Value{
		"policy_set_id": tftypes.NewValue(tftypes.String, policySetID),
		"id":            tftypes.NewValue(tftypes.String, attachID),
	})

	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: configValue}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}

	d.Read(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state PolicyAttachmentDataSourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("unexpected error getting state: %v", diags)
	}

	if state.ID.ValueString() != attachID {
		t.Errorf("ID = %q, want %q", state.ID.ValueString(), attachID)
	}
	if state.WorkspaceId.ValueString() != "ws-10" {
		t.Errorf("WorkspaceId = %q, want ws-10", state.WorkspaceId.ValueString())
	}
}

func TestPolicyAttachmentDataSource_ReadByWorkspaceId(t *testing.T) {
	ctx := context.Background()
	s, objType := policyAttachmentDataSourceSchemaAndType(t, ctx)

	const policySetID = "ps-100"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/v1/policy_set/%s/attachments", policySetID), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"data":[
			{
				"type":"policy_attachment",
				"id":"att-project",
				"relationships":{"project":{"data":{"type":"project","id":"proj-20"}}}
			},
			{
				"type":"policy_attachment",
				"id":"att-ws",
				"relationships":{"workspace":{"data":{"type":"workspace","id":"ws-20"}}}
			}
		]}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	d := &PolicyAttachmentDataSource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	configValue := buildObjectValue(objType, map[string]tftypes.Value{
		"policy_set_id": tftypes.NewValue(tftypes.String, policySetID),
		"workspace_id":  tftypes.NewValue(tftypes.String, "ws-20"),
	})

	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: configValue}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}

	d.Read(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state PolicyAttachmentDataSourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("unexpected error getting state: %v", diags)
	}

	if state.ID.ValueString() != "att-ws" {
		t.Errorf("ID = %q, want att-ws", state.ID.ValueString())
	}
	if state.WorkspaceId.ValueString() != "ws-20" {
		t.Errorf("WorkspaceId = %q, want ws-20", state.WorkspaceId.ValueString())
	}
}

func TestPolicyAttachmentDataSource_ReadByProjectId(t *testing.T) {
	ctx := context.Background()
	s, objType := policyAttachmentDataSourceSchemaAndType(t, ctx)

	const policySetID = "ps-100"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/v1/policy_set/%s/attachments", policySetID), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"data":[
			{
				"type":"policy_attachment",
				"id":"att-proj-1",
				"relationships":{"project":{"data":{"type":"project","id":"proj-99"}}}
			}
		]}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	d := &PolicyAttachmentDataSource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	configValue := buildObjectValue(objType, map[string]tftypes.Value{
		"policy_set_id": tftypes.NewValue(tftypes.String, policySetID),
		"project_id":    tftypes.NewValue(tftypes.String, "proj-99"),
	})

	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: configValue}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}

	d.Read(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state PolicyAttachmentDataSourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("unexpected error getting state: %v", diags)
	}

	if state.ID.ValueString() != "att-proj-1" {
		t.Errorf("ID = %q, want att-proj-1", state.ID.ValueString())
	}
	if state.ProjectId.ValueString() != "proj-99" {
		t.Errorf("ProjectId = %q, want proj-99", state.ProjectId.ValueString())
	}
}

func TestPolicyAttachmentDataSource_ReadByTagId(t *testing.T) {
	ctx := context.Background()
	s, objType := policyAttachmentDataSourceSchemaAndType(t, ctx)

	const policySetID = "ps-100"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/v1/policy_set/%s/attachments", policySetID), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"data":[
			{
				"type":"policy_attachment",
				"id":"att-tag-1",
				"relationships":{"tag":{"data":{"type":"tag","id":"tag-55"}}}
			}
		]}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	d := &PolicyAttachmentDataSource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	configValue := buildObjectValue(objType, map[string]tftypes.Value{
		"policy_set_id": tftypes.NewValue(tftypes.String, policySetID),
		"tag_id":        tftypes.NewValue(tftypes.String, "tag-55"),
	})

	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: configValue}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}

	d.Read(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state PolicyAttachmentDataSourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("unexpected error getting state: %v", diags)
	}

	if state.ID.ValueString() != "att-tag-1" {
		t.Errorf("ID = %q, want att-tag-1", state.ID.ValueString())
	}
	if state.TagId.ValueString() != "tag-55" {
		t.Errorf("TagId = %q, want tag-55", state.TagId.ValueString())
	}
}

func TestPolicyAttachmentDataSource_MissingCriteria(t *testing.T) {
	ctx := context.Background()
	s, objType := policyAttachmentDataSourceSchemaAndType(t, ctx)

	d := &PolicyAttachmentDataSource{}

	configValue := buildObjectValue(objType, map[string]tftypes.Value{
		"policy_set_id": tftypes.NewValue(tftypes.String, "ps-100"),
	})

	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: configValue}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}

	d.Read(ctx, req, resp)

	if !resp.Diagnostics.HasError() {
		t.Errorf("expected error when no search criteria is provided")
	}
}
