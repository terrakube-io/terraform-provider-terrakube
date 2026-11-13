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

func TestOrganizationVariableResourceSensitiveResponse(t *testing.T) {
	for _, operation := range []string{"Create", "Read", "Update"} {
		for _, tc := range []struct {
			name, attribute string
			sensitive       bool
		}{
			{"omitted", "", false},
			{"null", `,"sensitive":null`, false},
			{"false", `,"sensitive":false`, false},
			{"true", `,"sensitive":true`, true},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				ctx := context.Background()
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					w.Header().Set("Content-Type", "application/vnd.api+json")
					fmt.Fprintf(w, `{"data":{"type":"globalvar","id":"var-1","attributes":{"key":"example","value":"server-value","category":"ENV","hcl":false%s}}}`, tc.attribute)
				}))
				defer server.Close()
				r := &OrganizationVariableResource{client: server.Client(), endpoint: server.URL, token: "test-token"}
				var schemaResp resource.SchemaResponse
				r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
				s := schemaResp.Schema
				objType, ok := s.Type().TerraformType(ctx).(tftypes.Object)
				if !ok {
					t.Fatal("expected object schema")
				}
				raw := buildObjectValue(objType, map[string]tftypes.Value{
					"id":              tftypes.NewValue(tftypes.String, "var-1"),
					"organization_id": tftypes.NewValue(tftypes.String, "org-1"),
					"key":             tftypes.NewValue(tftypes.String, "example"),
					"value":           tftypes.NewValue(tftypes.String, "original-value"),
					"category":        tftypes.NewValue(tftypes.String, "ENV"),
					"sensitive":       tftypes.NewValue(tftypes.Bool, tc.sensitive),
					"hcl":             tftypes.NewValue(tftypes.Bool, false),
				})
				state := tfsdk.State{Schema: s, Raw: raw}
				plan := tfsdk.Plan{Schema: s, Raw: raw}
				switch operation {
				case "Create":
					resp := resource.CreateResponse{State: state}
					r.Create(ctx, resource.CreateRequest{Plan: plan}, &resp)
					if resp.Diagnostics.HasError() {
						t.Fatal(resp.Diagnostics)
					}
					state = resp.State
				case "Read":
					resp := resource.ReadResponse{State: state}
					r.Read(ctx, resource.ReadRequest{State: state}, &resp)
					if resp.Diagnostics.HasError() {
						t.Fatal(resp.Diagnostics)
					}
					state = resp.State
				case "Update":
					resp := resource.UpdateResponse{State: state}
					r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state}, &resp)
					if resp.Diagnostics.HasError() {
						t.Fatal(resp.Diagnostics)
					}
					state = resp.State
				}
				var got OrganizationVariableResourceModel
				if diags := state.Get(ctx, &got); diags.HasError() {
					t.Fatal(diags)
				}
				if got.Sensitive.IsNull() || got.Sensitive.ValueBool() != tc.sensitive {
					t.Errorf("sensitive = %v, want %v", got.Sensitive, tc.sensitive)
				}
				want := "server-value"
				if tc.sensitive {
					want = "original-value"
				}
				if got.Value.ValueString() != want {
					t.Errorf("value = %q, want %q", got.Value.ValueString(), want)
				}
			})
		}
	}
}
