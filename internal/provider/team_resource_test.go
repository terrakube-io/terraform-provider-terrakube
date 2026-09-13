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

func teamResourceSchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	r := &TeamResource{}
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

func TestTeamResource_Schema(t *testing.T) {
	ctx := context.Background()
	s, _ := teamResourceSchemaAndType(t, ctx)

	mpAttr, ok := s.Attributes["manage_policies"]
	if !ok {
		t.Fatalf("expected manage_policies attribute to exist on team resource")
	}
	if !mpAttr.IsOptional() {
		t.Errorf("expected manage_policies to be optional")
	}
	if !mpAttr.IsComputed() {
		t.Errorf("expected manage_policies to be computed")
	}
}

func TestTeamResource_Create_WithManagePolicies(t *testing.T) {
	ctx := context.Background()
	s, objType := teamResourceSchemaAndType(t, ctx)

	const orgID = "org-1"
	var receivedBody string

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/v1/organization/%s/team", orgID), func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)

		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"data":{"type":"team","id":"team-10","attributes":{
			"name":"secops",
			"managePolicies":true,
			"manageState":false
		}}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &TeamResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	planValue := buildObjectValue(objType, map[string]tftypes.Value{
		"organization_id": tftypes.NewValue(tftypes.String, orgID),
		"name":            tftypes.NewValue(tftypes.String, "secops"),
		"manage_policies": tftypes.NewValue(tftypes.Bool, true),
	})

	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValue}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

	r.Create(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state TeamResourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("unexpected error getting state: %v", diags)
	}

	if !state.ManagePolicies.ValueBool() {
		t.Errorf("ManagePolicies = false, want true")
	}
	if !strings.Contains(receivedBody, `"managePolicies":true`) {
		t.Errorf("expected request body to contain managePolicies:true, got: %s", receivedBody)
	}
}

func TestTeamResource_Create_BackwardCompatibility_OmitWhenUnset(t *testing.T) {
	ctx := context.Background()
	s, objType := teamResourceSchemaAndType(t, ctx)

	const orgID = "org-1"
	var receivedBody string

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/v1/organization/%s/team", orgID), func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)

		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"data":{"type":"team","id":"team-11","attributes":{
			"name":"devs",
			"manageState":false
		}}}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	r := &TeamResource{
		client:   server.Client(),
		endpoint: server.URL,
		token:    "test-token",
	}

	// manage_policies is omitted / unset in planValue
	planValue := buildObjectValue(objType, map[string]tftypes.Value{
		"organization_id": tftypes.NewValue(tftypes.String, orgID),
		"name":            tftypes.NewValue(tftypes.String, "devs"),
	})

	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValue}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

	r.Create(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var state TeamResourceModel
	if diags := resp.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("unexpected error getting state: %v", diags)
	}

	if strings.Contains(receivedBody, "managePolicies") {
		t.Errorf("expected managePolicies to be omitted from payload for older APIs, got: %s", receivedBody)
	}
	if !state.ManagePolicies.IsNull() {
		t.Errorf("expected ManagePolicies in state to be null when omitted by API response, got: %v", state.ManagePolicies)
	}
}
