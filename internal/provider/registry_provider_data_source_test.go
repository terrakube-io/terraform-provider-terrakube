package provider

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestRegistryProviderDataSource(t *testing.T) {
	runLookupDataSourceTests(t, lookupDataSourceCase{
		apiPath:    "/api/v1/organization/org-1/provider",
		filterKey:  "filter[provider]",
		entityType: "provider",
		config:     map[string]string{"organization_id": "org-1"},
		matchAttrs: `"description":"AWS provider","imported":true,"registryNamespace":"hashicorp"`,
		wantState:  map[string]string{"id": "obj-1", "description": "AWS provider", "registry_namespace": "hashicorp"},
		notFound:   "Registry provider not found",
		multiple:   "Multiple registry provider entries found",
		apiError:   "Error reading registry provider: 403 Forbidden",
		badPayload: "Unable to unmarshal registry provider payload",
		newSource: func(c *http.Client, endpoint string) datasource.DataSource {
			return &RegistryProviderDataSource{client: c, endpoint: endpoint, token: "test-token"}
		},
	})
}

// imported is false, not null, for a provider that is not mirrored and for a Terrakube older
// than 2.30.0, which does not return it.
func TestRegistryProviderDataSource_ImportedAndOptionalAttributes(t *testing.T) {
	for _, test := range []struct {
		name         string
		attrs        string
		wantImported bool
	}{
		{"imported", `,"imported":true,"registryNamespace":"hashicorp"`, true},
		{"not imported", `,"imported":false`, false},
		{"an older Terrakube", ``, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			tc := lookupDataSourceCase{
				config: map[string]string{"organization_id": "org-1"},
				newSource: func(c *http.Client, endpoint string) datasource.DataSource {
					return &RegistryProviderDataSource{client: c, endpoint: endpoint, token: "test-token"}
				},
				entityType: "provider",
			}
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `{"data":[{"type":"provider","id":"obj-1","attributes":{"name":`+fmt.Sprintf("%q", lookupTestName)+test.attrs+`}}]}`)
			})
			resp := tc.read(t, handler)
			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
			}
			var imported types.Bool
			var namespace, description types.String
			ctx := context.Background()
			resp.State.GetAttribute(ctx, path.Root("imported"), &imported)
			resp.State.GetAttribute(ctx, path.Root("registry_namespace"), &namespace)
			resp.State.GetAttribute(ctx, path.Root("description"), &description)
			if imported.IsNull() || imported.ValueBool() != test.wantImported {
				t.Errorf("imported = %v, want %v", imported, test.wantImported)
			}
			if test.wantImported == (namespace.IsNull()) {
				t.Errorf("registry_namespace = %v", namespace)
			}
			if !description.IsNull() {
				t.Errorf("description should be null when the provider has none, got %v", description)
			}
		})
	}
}
