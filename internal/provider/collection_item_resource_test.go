package provider

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const (
	collectionItemTestOrg        = "org-1"
	collectionItemTestCollection = "col-1"
	collectionItemTestSecret     = "s3cr3t-value"
)

func collectionItemResourceSchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	r := &CollectionItemResource{}
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

// collectionItemServer answers the create (POST) and read (GET) calls with a
// sensitive item, as Terrakube does: no value in the response. It records the
// last request body.
func collectionItemServer(t *testing.T, captured *[]byte) *httptest.Server {
	t.Helper()

	base := "/api/v1/organization/" + collectionItemTestOrg + "/collection/" + collectionItemTestCollection + "/item"
	response := `{"data":{"type":"item","id":"item-1","attributes":{` +
		`"key":"SOPS_AGE_KEY","value":"","category":"ENV","sensitive":true,"hcl":false}}}`

	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, req *http.Request) {
		if req.Body != nil {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("reading request body: %v", err)
			}
			if len(body) > 0 {
				*captured = body
			}
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		fmt.Fprint(w, response)
	}
	mux.HandleFunc(base, handler)
	mux.HandleFunc(base+"/item-1", handler)

	return httptest.NewServer(mux)
}

func collectionItemValues(objType tftypes.Object, overrides map[string]tftypes.Value) tftypes.Value {
	values := map[string]tftypes.Value{
		"organization_id": tftypes.NewValue(tftypes.String, collectionItemTestOrg),
		"collection_id":   tftypes.NewValue(tftypes.String, collectionItemTestCollection),
		"key":             tftypes.NewValue(tftypes.String, "SOPS_AGE_KEY"),
		"category":        tftypes.NewValue(tftypes.String, "ENV"),
		"sensitive":       tftypes.NewValue(tftypes.Bool, true),
		"hcl":             tftypes.NewValue(tftypes.Bool, false),
	}
	maps.Copy(values, overrides)
	return buildObjectValue(objType, values)
}

func TestCollectionItemResource_Schema(t *testing.T) {
	ctx := context.Background()
	s, _ := collectionItemResourceSchemaAndType(t, ctx)

	value := s.Attributes["value"]
	if !value.IsOptional() || !value.IsSensitive() {
		t.Errorf("expected value to be optional and sensitive")
	}

	valueWo := s.Attributes["value_wo"]
	if !valueWo.IsOptional() || !valueWo.IsSensitive() || !valueWo.IsWriteOnly() {
		t.Errorf("expected value_wo to be optional, sensitive and write-only")
	}

	if !s.Attributes["value_wo_version"].IsOptional() {
		t.Errorf("expected value_wo_version to be optional")
	}
}

func TestCollectionItemResource_Create_ValueWo(t *testing.T) {
	ctx := context.Background()
	s, objType := collectionItemResourceSchemaAndType(t, ctx)

	var captured []byte
	server := collectionItemServer(t, &captured)
	defer server.Close()

	r := &CollectionItemResource{client: server.Client(), endpoint: server.URL, token: "test-token"}

	// Write-only values are null in the plan; only the configuration has them.
	planValue := collectionItemValues(objType, map[string]tftypes.Value{
		"value_wo_version": tftypes.NewValue(tftypes.Number, 1),
	})
	configValue := collectionItemValues(objType, map[string]tftypes.Value{
		"value_wo":         tftypes.NewValue(tftypes.String, collectionItemTestSecret),
		"value_wo_version": tftypes.NewValue(tftypes.Number, 1),
	})

	req := resource.CreateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: planValue},
		Config: tfsdk.Config{Schema: s, Raw: configValue},
	}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

	r.Create(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics)
	}
	if !strings.Contains(string(captured), collectionItemTestSecret) {
		t.Errorf("expected the request body to carry value_wo, got: %s", captured)
	}

	var result CollectionItemResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}
	if !result.Value.IsNull() {
		t.Errorf("expected value to stay null in state, got: %q", result.Value.ValueString())
	}
	if !result.ValueWo.IsNull() {
		t.Errorf("expected value_wo to be null in state")
	}
	if result.ValueWoVersion.ValueInt64() != 1 {
		t.Errorf("expected value_wo_version 1 in state, got: %d", result.ValueWoVersion.ValueInt64())
	}
	if result.ID.ValueString() != "item-1" {
		t.Errorf("expected ID item-1, got: %q", result.ID.ValueString())
	}
}

func TestCollectionItemResource_Create_Value(t *testing.T) {
	ctx := context.Background()
	s, objType := collectionItemResourceSchemaAndType(t, ctx)

	var captured []byte
	server := collectionItemServer(t, &captured)
	defer server.Close()

	r := &CollectionItemResource{client: server.Client(), endpoint: server.URL, token: "test-token"}

	values := collectionItemValues(objType, map[string]tftypes.Value{
		"value": tftypes.NewValue(tftypes.String, collectionItemTestSecret),
	})
	req := resource.CreateRequest{
		Plan:   tfsdk.Plan{Schema: s, Raw: values},
		Config: tfsdk.Config{Schema: s, Raw: values},
	}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

	r.Create(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics)
	}
	if !strings.Contains(string(captured), collectionItemTestSecret) {
		t.Errorf("expected the request body to carry value, got: %s", captured)
	}

	var result CollectionItemResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}
	// Unchanged behaviour: a sensitive item keeps the configured value in state.
	if result.Value.ValueString() != collectionItemTestSecret {
		t.Errorf("expected value preserved in state, got: %q", result.Value.ValueString())
	}
}

func TestCollectionItemResource_Update_ValueWo(t *testing.T) {
	ctx := context.Background()
	s, objType := collectionItemResourceSchemaAndType(t, ctx)

	var captured []byte
	server := collectionItemServer(t, &captured)
	defer server.Close()

	r := &CollectionItemResource{client: server.Client(), endpoint: server.URL, token: "test-token"}

	stateValue := collectionItemValues(objType, map[string]tftypes.Value{
		"id":               tftypes.NewValue(tftypes.String, "item-1"),
		"value_wo_version": tftypes.NewValue(tftypes.Number, 1),
	})
	planValue := collectionItemValues(objType, map[string]tftypes.Value{
		"id":               tftypes.NewValue(tftypes.String, "item-1"),
		"value_wo_version": tftypes.NewValue(tftypes.Number, 2),
	})
	configValue := collectionItemValues(objType, map[string]tftypes.Value{
		"value_wo":         tftypes.NewValue(tftypes.String, "rotated-value"),
		"value_wo_version": tftypes.NewValue(tftypes.Number, 2),
	})

	req := resource.UpdateRequest{
		State:  tfsdk.State{Schema: s, Raw: stateValue},
		Plan:   tfsdk.Plan{Schema: s, Raw: planValue},
		Config: tfsdk.Config{Schema: s, Raw: configValue},
	}
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s}}

	r.Update(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Update returned diagnostics: %v", resp.Diagnostics)
	}
	if !strings.Contains(string(captured), "rotated-value") {
		t.Errorf("expected the PATCH body to carry the new value_wo, got: %s", captured)
	}

	var result CollectionItemResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}
	if !result.Value.IsNull() || !result.ValueWo.IsNull() {
		t.Errorf("expected value and value_wo to stay null in state")
	}
	if result.ValueWoVersion.ValueInt64() != 2 {
		t.Errorf("expected value_wo_version 2 in state, got: %d", result.ValueWoVersion.ValueInt64())
	}
}

func TestCollectionItemResource_Read_KeepsNullValue(t *testing.T) {
	ctx := context.Background()
	s, objType := collectionItemResourceSchemaAndType(t, ctx)

	var captured []byte
	server := collectionItemServer(t, &captured)
	defer server.Close()

	r := &CollectionItemResource{client: server.Client(), endpoint: server.URL, token: "test-token"}

	stateValue := collectionItemValues(objType, map[string]tftypes.Value{
		"id": tftypes.NewValue(tftypes.String, "item-1"),
	})
	req := resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: stateValue}}
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: stateValue}}

	r.Read(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics)
	}

	var result CollectionItemResourceModel
	if diags := resp.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("reading resulting state: %v", diags)
	}
	// Before value_wo, a null value became "" here and showed as a diff.
	if !result.Value.IsNull() {
		t.Errorf("expected value to stay null after read, got: %q", result.Value.ValueString())
	}
}

func TestCollectionItemResource_ValidateConfig(t *testing.T) {
	ctx := context.Background()
	s, objType := collectionItemResourceSchemaAndType(t, ctx)
	r := &CollectionItemResource{}

	cases := map[string]struct {
		overrides map[string]tftypes.Value
		wantError bool
	}{
		"value_wo with sensitive=false": {
			overrides: map[string]tftypes.Value{
				"value_wo":  tftypes.NewValue(tftypes.String, collectionItemTestSecret),
				"sensitive": tftypes.NewValue(tftypes.Bool, false),
			},
			wantError: true,
		},
		"value_wo with sensitive=true": {
			overrides: map[string]tftypes.Value{
				"value_wo": tftypes.NewValue(tftypes.String, collectionItemTestSecret),
			},
		},
		"value with sensitive=false": {
			overrides: map[string]tftypes.Value{
				"value":     tftypes.NewValue(tftypes.String, "plain"),
				"sensitive": tftypes.NewValue(tftypes.Bool, false),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := resource.ValidateConfigRequest{
				Config: tfsdk.Config{Schema: s, Raw: collectionItemValues(objType, tc.overrides)},
			}
			resp := &resource.ValidateConfigResponse{}

			r.ValidateConfig(ctx, req, resp)

			if resp.Diagnostics.HasError() != tc.wantError {
				t.Errorf("expected error=%v, got diagnostics: %v", tc.wantError, resp.Diagnostics)
			}
		})
	}
}

func TestCollectionItemResource_ExactlyOneValue(t *testing.T) {
	ctx := context.Background()
	s, objType := collectionItemResourceSchemaAndType(t, ctx)
	r := &CollectionItemResource{}

	cases := map[string]struct {
		overrides map[string]tftypes.Value
		wantError bool
	}{
		"neither value nor value_wo": {wantError: true},
		"both value and value_wo": {
			overrides: map[string]tftypes.Value{
				"value":    tftypes.NewValue(tftypes.String, "a"),
				"value_wo": tftypes.NewValue(tftypes.String, "b"),
			},
			wantError: true,
		},
		"only value_wo": {
			overrides: map[string]tftypes.Value{
				"value_wo": tftypes.NewValue(tftypes.String, "b"),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := resource.ValidateConfigRequest{
				Config: tfsdk.Config{Schema: s, Raw: collectionItemValues(objType, tc.overrides)},
			}

			var gotError bool
			for _, v := range r.ConfigValidators(ctx) {
				resp := &resource.ValidateConfigResponse{}
				v.ValidateResource(ctx, req, resp)
				gotError = gotError || resp.Diagnostics.HasError()
			}

			if gotError != tc.wantError {
				t.Errorf("expected error=%v, got %v", tc.wantError, gotError)
			}
		})
	}
}
