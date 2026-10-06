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
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// workspaceCliSchemaAndType returns the resource's schema along with the
// tftypes.Object type it maps to, so tests can build raw plan/state values
// without hand-maintaining a duplicate of the attribute list.
func workspaceCliSchemaAndType(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object) {
	t.Helper()

	r := &WorkspaceCliResource{}
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

// TestWorkspaceCliResource_Create_OmitsRelationshipWhenFieldUnset mirrors
// TestWorkspaceVcsResource_Create_OmitsRelationshipWhenFieldUnset: project_id
// and agent_id are both Optional+Computed with no Default, so Terraform's
// plan leaves them *unknown* (not null) during Create when absent from
// config. A guard that only checks IsNull() would send a relationship with
// an empty id, which the API rejects.
func TestWorkspaceCliResource_Create_OmitsRelationshipWhenFieldUnset(t *testing.T) {
	tests := []struct {
		name            string
		attrName        string
		relationshipKey string
		getField        func(WorkspaceCliResourceModel) types.String
	}{
		{"project", "project_id", "project", func(m WorkspaceCliResourceModel) types.String { return m.ProjectId }},
		{"agent", "agent_id", "agent", func(m WorkspaceCliResourceModel) types.String { return m.AgentId }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s, objType := workspaceCliSchemaAndType(t, ctx)

			const orgID = "org-1"

			var capturedBody []byte
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v1/organization/"+orgID+"/workspace", func(w http.ResponseWriter, req *http.Request) {
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatalf("reading request body: %v", err)
				}
				capturedBody = body
				fmt.Fprintf(w, `{"data":{"type":"workspace","id":"ws-created-1","attributes":{`+
					`"name":"my-workspace","description":null,"iacType":"terraform",`+
					`"terraformVersion":"1.12.0","executionMode":"remote"},`+
					`"relationships":{ "%s":{"data":null}}}}`, tc.relationshipKey)
			})

			server := httptest.NewServer(mux)
			defer server.Close()

			r := &WorkspaceCliResource{
				client:   server.Client(),
				endpoint: server.URL,
				token:    "test-token",
			}

			planValue := buildObjectValue(objType, map[string]tftypes.Value{
				"organization_id": tftypes.NewValue(tftypes.String, orgID),
				"name":            tftypes.NewValue(tftypes.String, "my-workspace"),
				"iac_version":     tftypes.NewValue(tftypes.String, "1.12.0"),
				"iac_type":        tftypes.NewValue(tftypes.String, "terraform"),
				"execution_mode":  tftypes.NewValue(tftypes.String, "remote"),
				// The attribute below is intentionally left unknown, matching what
				// Terraform actually produces for an Optional+Computed attribute
				// that's absent from config during Create (never null).
				tc.attrName: tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
			})

			req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: planValue}}
			resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

			r.Create(ctx, req, resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics)
			}

			if strings.Contains(string(capturedBody), fmt.Sprintf(`"%s"`, tc.relationshipKey)) {
				t.Errorf("expected request body to omit the %s relationship when %s is unset, got: %s", tc.relationshipKey, tc.attrName, capturedBody)
			}

			var result WorkspaceCliResourceModel
			if diags := resp.State.Get(ctx, &result); diags.HasError() {
				t.Fatalf("reading resulting state: %v", diags)
			}

			if tc.getField(result).IsUnknown() {
				t.Errorf("expected %s to be resolved to null after create, but it is still unknown (Terraform's protocol rejects unknown values after apply)", tc.attrName)
			}
			if !tc.getField(result).IsNull() {
				t.Errorf("expected %s to be null after create since the API returned no %s, got: %v", tc.attrName, tc.relationshipKey, tc.getField(result))
			}
		})
	}
}
