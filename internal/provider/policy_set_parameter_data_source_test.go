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

func policySetParameterDataSourceSchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	d := &PolicySetParameterDataSource{}
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

func TestPolicySetParameterDataSource_ReadById(t *testing.T) {
	ctx := context.Background()
	s, objType := policySetParameterDataSourceSchemaAndType(t, ctx)

	const policySetID = "ps-100"
	const paramID = "param-1"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/v1/policy_set/%s/parameters/%s", policySetID, paramID), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"data":{"type":"policy_set_parameter","id":"param-1","attributes":{
			"key":"max_vcpus",
			"value":"64",
			"description":"Max allowed vCPUs"
		}}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	d := &PolicySetParameterDataSource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	configValue := buildObjectValue(objType, map[string]tftypes.Value{
		"policy_set_id": tftypes.NewValue(tftypes.String, policySetID),
		"id":            tftypes.NewValue(tftypes.String, paramID),
	})

	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: configValue}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}

	d.Read(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state PolicySetParameterDataSourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("unexpected error getting state: %v", diags)
	}

	if state.ID.ValueString() != paramID {
		t.Errorf("ID = %q, want %q", state.ID.ValueString(), paramID)
	}
	if state.Key.ValueString() != "max_vcpus" {
		t.Errorf("Key = %q, want max_vcpus", state.Key.ValueString())
	}
	if state.Value.ValueString() != "64" {
		t.Errorf("Value = %q, want 64", state.Value.ValueString())
	}
}

func TestPolicySetParameterDataSource_ReadByKey(t *testing.T) {
	ctx := context.Background()
	s, objType := policySetParameterDataSourceSchemaAndType(t, ctx)

	const policySetID = "ps-100"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/v1/policy_set/%s/parameters", policySetID), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"data":[
			{
				"type":"policy_set_parameter",
				"id":"param-1",
				"attributes":{
					"key":"other_param",
					"value":"abc"
				}
			},
			{
				"type":"policy_set_parameter",
				"id":"param-2",
				"attributes":{
					"key":"allowed_regions",
					"value":"[\"us-east-1\",\"us-west-2\"]",
					"description":"Target regions"
				}
			}
		]}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	d := &PolicySetParameterDataSource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	configValue := buildObjectValue(objType, map[string]tftypes.Value{
		"policy_set_id": tftypes.NewValue(tftypes.String, policySetID),
		"key":           tftypes.NewValue(tftypes.String, "allowed_regions"),
	})

	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: configValue}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}

	d.Read(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state PolicySetParameterDataSourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("unexpected error getting state: %v", diags)
	}

	if state.ID.ValueString() != "param-2" {
		t.Errorf("ID = %q, want param-2", state.ID.ValueString())
	}
	if state.Key.ValueString() != "allowed_regions" {
		t.Errorf("Key = %q, want allowed_regions", state.Key.ValueString())
	}
	if state.Value.ValueString() != "[\"us-east-1\",\"us-west-2\"]" {
		t.Errorf("Value = %q, want list", state.Value.ValueString())
	}
}

func TestPolicySetParameterDataSource_MissingCriteria(t *testing.T) {
	ctx := context.Background()
	s, objType := policySetParameterDataSourceSchemaAndType(t, ctx)

	d := &PolicySetParameterDataSource{}

	configValue := buildObjectValue(objType, map[string]tftypes.Value{
		"policy_set_id": tftypes.NewValue(tftypes.String, "ps-100"),
	})

	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: configValue}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}

	d.Read(ctx, req, resp)

	if !resp.Diagnostics.HasError() {
		t.Errorf("expected error when neither id nor key is provided")
	}
}

func TestPolicySetParameterDataSource_NotFound(t *testing.T) {
	ctx := context.Background()
	s, objType := policySetParameterDataSourceSchemaAndType(t, ctx)

	const policySetID = "ps-100"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/v1/policy_set/%s/parameters", policySetID), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"data":[]}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	d := &PolicySetParameterDataSource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	configValue := buildObjectValue(objType, map[string]tftypes.Value{
		"policy_set_id": tftypes.NewValue(tftypes.String, policySetID),
		"key":           tftypes.NewValue(tftypes.String, "nonexistent"),
	})

	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: configValue}}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}

	d.Read(ctx, req, resp)

	if !resp.Diagnostics.HasError() {
		t.Errorf("expected error when parameter key is not found")
	}
}
