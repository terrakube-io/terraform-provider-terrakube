package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func policySetParameterResourceSchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	r := &PolicySetParameterResource{}
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

func TestPolicySetParameterResource_Schema(t *testing.T) {
	ctx := context.Background()
	s, _ := policySetParameterResourceSchemaAndType(t, ctx)

	requiredAttrs := []string{"policy_set_id", "key", "value"}
	for _, attr := range requiredAttrs {
		a, ok := s.Attributes[attr]
		if !ok {
			t.Errorf("expected attribute %q to exist", attr)
		} else if !a.IsRequired() {
			t.Errorf("expected attribute %q to be required", attr)
		}
	}

	if !s.Attributes["id"].IsComputed() {
		t.Errorf("expected id to be computed")
	}
	if !s.Attributes["description"].IsOptional() {
		t.Errorf("expected description to be optional")
	}
}

func TestPolicySetParameterResource_Create(t *testing.T) {
	ctx := context.Background()
	s, objType := policySetParameterResourceSchemaAndType(t, ctx)

	const policySetID = "ps-100"
	var receivedBody string

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/v1/policy_set/%s/parameters", policySetID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)

		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"data":{"type":"policy_set_parameter","id":"param-1","attributes":{
			"key":"max_vcpus",
			"value":"64",
			"description":"Max allowed vCPUs"
		}}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &PolicySetParameterResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	planValue := buildObjectValue(objType, map[string]tftypes.Value{
		"policy_set_id": tftypes.NewValue(tftypes.String, policySetID),
		"key":           tftypes.NewValue(tftypes.String, "max_vcpus"),
		"value":         tftypes.NewValue(tftypes.String, "64"),
		"description":   tftypes.NewValue(tftypes.String, "Max allowed vCPUs"),
	})

	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValue}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

	r.Create(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state PolicySetParameterResourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("unexpected error getting state: %v", diags)
	}

	if state.ID.ValueString() != "param-1" {
		t.Errorf("ID = %q, want param-1", state.ID.ValueString())
	}
	if state.Key.ValueString() != "max_vcpus" {
		t.Errorf("Key = %q, want max_vcpus", state.Key.ValueString())
	}
	if state.Value.ValueString() != "64" {
		t.Errorf("Value = %q, want 64", state.Value.ValueString())
	}
	if state.Description.ValueString() != "Max allowed vCPUs" {
		t.Errorf("Description = %q, want Max allowed vCPUs", state.Description.ValueString())
	}
	if receivedBody == "" {
		t.Errorf("expected non-empty received request body")
	}
}

func TestPolicySetParameterResource_Read(t *testing.T) {
	ctx := context.Background()
	s, objType := policySetParameterResourceSchemaAndType(t, ctx)

	const policySetID = "ps-100"
	const paramID = "param-1"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/v1/policy_set/%s/parameters/%s", policySetID, paramID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"data":{"type":"policy_set_parameter","id":"param-1","attributes":{
			"key":"max_vcpus",
			"value":"64",
			"description":"Max allowed vCPUs"
		}}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &PolicySetParameterResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	stateValue := buildObjectValue(objType, map[string]tftypes.Value{
		"id":            tftypes.NewValue(tftypes.String, paramID),
		"policy_set_id": tftypes.NewValue(tftypes.String, policySetID),
		"key":           tftypes.NewValue(tftypes.String, "max_vcpus"),
		"value":         tftypes.NewValue(tftypes.String, "64"),
		"description":   tftypes.NewValue(tftypes.String, "Max allowed vCPUs"),
	})

	req := resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: stateValue}}
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: stateValue}}

	r.Read(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state PolicySetParameterResourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("unexpected error getting state: %v", diags)
	}

	if state.ID.ValueString() != paramID {
		t.Errorf("ID = %q, want %q", state.ID.ValueString(), paramID)
	}
}

func TestPolicySetParameterResource_Update(t *testing.T) {
	ctx := context.Background()
	s, objType := policySetParameterResourceSchemaAndType(t, ctx)

	const policySetID = "ps-100"
	const paramID = "param-1"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/v1/policy_set/%s/parameters/%s", policySetID, paramID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("unexpected method %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"data":{"type":"policy_set_parameter","id":"param-1","attributes":{
			"key":"max_vcpus",
			"value":"128",
			"description":"Updated vCPU limit"
		}}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &PolicySetParameterResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	planValue := buildObjectValue(objType, map[string]tftypes.Value{
		"id":            tftypes.NewValue(tftypes.String, paramID),
		"policy_set_id": tftypes.NewValue(tftypes.String, policySetID),
		"key":           tftypes.NewValue(tftypes.String, "max_vcpus"),
		"value":         tftypes.NewValue(tftypes.String, "128"),
		"description":   tftypes.NewValue(tftypes.String, "Updated vCPU limit"),
	})

	req := resource.UpdateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValue}}
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s}}

	r.Update(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state PolicySetParameterResourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("unexpected error getting state: %v", diags)
	}

	if state.Value.ValueString() != "128" {
		t.Errorf("Value = %q, want 128", state.Value.ValueString())
	}
	if state.Description.ValueString() != "Updated vCPU limit" {
		t.Errorf("Description = %q, want Updated vCPU limit", state.Description.ValueString())
	}
}

func TestPolicySetParameterResource_Delete(t *testing.T) {
	ctx := context.Background()
	s, objType := policySetParameterResourceSchemaAndType(t, ctx)

	const policySetID = "ps-100"
	const paramID = "param-1"
	deleted := false

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/v1/policy_set/%s/parameters/%s", policySetID, paramID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Fatalf("unexpected method %s", r.Method)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &PolicySetParameterResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	stateValue := buildObjectValue(objType, map[string]tftypes.Value{
		"id":            tftypes.NewValue(tftypes.String, paramID),
		"policy_set_id": tftypes.NewValue(tftypes.String, policySetID),
	})

	req := resource.DeleteRequest{State: tfsdk.State{Schema: s, Raw: stateValue}}
	resp := &resource.DeleteResponse{State: tfsdk.State{Schema: s}}

	r.Delete(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if !deleted {
		t.Errorf("expected parameter to be deleted via DELETE request")
	}
}

func TestPolicySetParameterResource_Import(t *testing.T) {
	ctx := context.Background()
	s, objType := policySetParameterResourceSchemaAndType(t, ctx)

	r := &PolicySetParameterResource{}

	// Test slash separator
	resp := &resource.ImportStateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "ps-123/param-456"}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected error importing with slash: %v", resp.Diagnostics)
	}

	var psID, pID string
	resp.State.GetAttribute(ctx, path.Root("policy_set_id"), &psID)
	resp.State.GetAttribute(ctx, path.Root("id"), &pID)

	if psID != "ps-123" || pID != "param-456" {
		t.Errorf("got policy_set_id=%s, id=%s; want ps-123, param-456", psID, pID)
	}

	// Test comma separator
	respComma := &resource.ImportStateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "ps-123,param-456"}, respComma)
	if respComma.Diagnostics.HasError() {
		t.Fatalf("unexpected error importing with comma: %v", respComma.Diagnostics)
	}

	respComma.State.GetAttribute(ctx, path.Root("policy_set_id"), &psID)
	respComma.State.GetAttribute(ctx, path.Root("id"), &pID)

	if psID != "ps-123" || pID != "param-456" {
		t.Errorf("got policy_set_id=%s, id=%s; want ps-123, param-456", psID, pID)
	}
}
