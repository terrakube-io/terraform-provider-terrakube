package provider

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	terrakube "github.com/terrakube-io/terrakube-go"
)

// configuredClient returns the shared terrakube-go client from the provider data a resource or
// data source receives in Configure. It returns nil, with no diagnostic, while the provider is
// not configured yet. kind names the caller in the error, for example "Organization Resource".
func configuredClient(providerData any, diags *diag.Diagnostics, kind string) *terrakube.Client {
	if providerData == nil {
		return nil
	}

	data, ok := providerData.(*TerrakubeConnectionData)
	if !ok {
		diags.AddError(
			fmt.Sprintf("Unexpected %s Configure Type", kind),
			fmt.Sprintf("Expected *TerrakubeConnectionData, got: %T. Please report this issue to the provider developers.", providerData),
		)
		return nil
	}

	return data.Client
}
