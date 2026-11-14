package provider

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// Every registered resource and data source needs an example, a generated docs page, a
// unit test file and, for resources that implement import, an import.sh. The lists below
// are the known gaps. An entry that is no longer needed fails the test, so they can only
// shrink.

// Resources without a unit test file. See #209.
var resourcesWithoutTests = map[string]bool{
	"collection":                 true,
	"federated_credential":       true,
	"federated_credential_claim": true,
	"organization":               true,
	"organization_tag":           true,
	"organization_template":      true,
	"organization_variable":      true,
	"project":                    true,
	"project_access":             true,
	"self_hosted_agent":          true,
	"team_token":                 true,
	"workspace_access":           true,
	"workspace_variable":         true,
}

// Data sources without a unit test file. See #209.
var dataSourcesWithoutTests = map[string]bool{
	"federated_credential":  true,
	"organization":          true,
	"organization_tag":      true,
	"organization_template": true,
	"output":                true,
	"project":               true,
	"ssh":                   true,
	"team":                  true,
	"vcs":                   true,
}

// Resources whose ImportState returns an error, so there is nothing to document.
var resourcesWithoutImport = map[string]bool{
	"workspace_tag": true,
}

// Example directories that do not match a registered type. See #227.
var misnamedExampleDirs = map[string]bool{
	"resources/terrakube_organization_collection": true,
	"resources/terrakube_workspace_webhook":       true,
}

// Resources whose example directory has another name. See #227.
var resourcesWithRenamedExample = map[string]string{
	"collection": "terrakube_organization_collection",
}

func registeredTypes(t *testing.T) (resources, dataSources []string) {
	t.Helper()
	ctx := context.Background()
	p := New("test")()

	for _, f := range p.Resources(ctx) {
		var resp resource.MetadataResponse
		f().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "terrakube"}, &resp)
		resources = append(resources, strings.TrimPrefix(resp.TypeName, "terrakube_"))
	}
	for _, f := range p.DataSources(ctx) {
		var resp datasource.MetadataResponse
		f().Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "terrakube"}, &resp)
		dataSources = append(dataSources, strings.TrimPrefix(resp.TypeName, "terrakube_"))
	}
	sort.Strings(resources)
	sort.Strings(dataSources)
	return resources, dataSources
}

func repoFileExists(t *testing.T, rel string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join("..", "..", filepath.FromSlash(rel)))
	return err == nil
}

func hasTestFile(t *testing.T, prefix string) bool {
	t.Helper()
	matches, err := filepath.Glob(prefix + "*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	return len(matches) > 0
}

// checkAllowed fails when a check passes for an entry that is on the allow list, or fails for
// an entry that is not.
func checkAllowed(t *testing.T, ok bool, allowed bool, what string) {
	t.Helper()
	switch {
	case !ok && !allowed:
		t.Errorf("%s", what)
	case ok && allowed:
		t.Errorf("%s: this is fixed, remove it from the known gaps in repository_consistency_test.go", what)
	}
}

func TestRepository_ResourcesAreComplete(t *testing.T) {
	resources, _ := registeredTypes(t)
	if len(resources) == 0 {
		t.Fatal("no resources registered")
	}

	for _, name := range resources {
		t.Run(name, func(t *testing.T) {
			exampleDir := "terrakube_" + name
			if renamed, ok := resourcesWithRenamedExample[name]; ok {
				exampleDir = renamed
			}

			if !repoFileExists(t, "examples/resources/"+exampleDir+"/resource.tf") {
				t.Errorf("missing examples/resources/%s/resource.tf", exampleDir)
			}
			if !repoFileExists(t, "docs/resources/"+name+".md") {
				t.Errorf("missing docs/resources/%s.md, run go generate ./...", name)
			}

			noImport := resourcesWithoutImport[name]
			checkAllowed(t, repoFileExists(t, "examples/resources/"+exampleDir+"/import.sh"), noImport,
				"missing examples/resources/"+exampleDir+"/import.sh")

			checkAllowed(t, hasTestFile(t, name+"_resource"), resourcesWithoutTests[name],
				"no "+name+"_resource*_test.go")
		})
	}
}

func TestRepository_DataSourcesAreComplete(t *testing.T) {
	_, dataSources := registeredTypes(t)
	if len(dataSources) == 0 {
		t.Fatal("no data sources registered")
	}

	for _, name := range dataSources {
		t.Run(name, func(t *testing.T) {
			if !repoFileExists(t, "examples/data-sources/terrakube_"+name+"/data-source.tf") {
				t.Errorf("missing examples/data-sources/terrakube_%s/data-source.tf", name)
			}
			if !repoFileExists(t, "docs/data-sources/"+name+".md") {
				t.Errorf("missing docs/data-sources/%s.md, run go generate ./...", name)
			}
			checkAllowed(t, hasTestFile(t, name+"_data_source"), dataSourcesWithoutTests[name],
				"no "+name+"_data_source*_test.go")
		})
	}
}

// An example directory for a type that is not registered is dead documentation.
func TestRepository_ExamplesMatchRegisteredTypes(t *testing.T) {
	resources, dataSources := registeredTypes(t)
	registered := map[string]bool{}
	for _, name := range resources {
		registered["resources/terrakube_"+name] = true
	}
	for _, name := range dataSources {
		registered["data-sources/terrakube_"+name] = true
	}

	for _, kind := range []string{"resources", "data-sources"} {
		entries, err := os.ReadDir(filepath.Join("..", "..", "examples", kind))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dir := kind + "/" + entry.Name()
			checkAllowed(t, registered[dir], misnamedExampleDirs[dir],
				"examples/"+dir+" does not match a registered resource or data source")
		}
	}
}
