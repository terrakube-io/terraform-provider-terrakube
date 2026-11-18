package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
)

// Data sources without a description. #262 adds the one for output. An entry that is no longer
// needed fails the test, so the list can only shrink.
var dataSourcesWithoutDescription = map[string]bool{
	"terrakube_output": true,
}

// TestDataSourceSchemas_HaveDescription fails when a data source has no description, since its
// docs page would then start with the attribute list.
func TestDataSourceSchemas_HaveDescription(t *testing.T) {
	ctx := context.Background()
	for _, f := range New("test")().DataSources(ctx) {
		d := f()
		var meta datasource.MetadataResponse
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "terrakube"}, &meta)
		var resp datasource.SchemaResponse
		d.Schema(ctx, datasource.SchemaRequest{}, &resp)

		has := resp.Schema.GetDescription() != "" || resp.Schema.GetMarkdownDescription() != ""
		checkAllowed(t, has, dataSourcesWithoutDescription[meta.TypeName], meta.TypeName+" has no description")
	}
}
