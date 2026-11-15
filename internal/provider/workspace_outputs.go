package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"terraform-provider-terrakube/internal/client"

	"github.com/google/jsonapi"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// workspaceOutputsClient reads the outputs of the latest run of a workspace.
// It is shared by the terrakube_output data source and ephemeral resource.
type workspaceOutputsClient struct {
	client   *http.Client
	endpoint string
	token    string
}

// workspaceOutputs holds every output of a workspace (values) and the subset
// that is not marked sensitive (nonSensitive).
type workspaceOutputs struct {
	values       types.Dynamic
	nonSensitive types.Dynamic
}

// read returns the outputs of the workspace's latest run. found is false when
// the workspace has no run history yet, which is not an error.
func (c workspaceOutputsClient) read(ctx context.Context, organization, workspace string, diags *diag.Diagnostics) (outputs workspaceOutputs, found bool) {
	orgs := c.list(ctx, fmt.Sprintf("%s/api/v1/organization?filter[organization]=name==%s", c.endpoint, organization), new(client.OrganizationEntity), diags)
	if diags.HasError() {
		return outputs, false
	}
	if len(orgs) == 0 {
		diags.AddError(fmt.Sprintf("Organization %q not found!", organization), fmt.Sprintf("%q", organization))
		return outputs, false
	}
	var organizationID string
	for _, organization := range orgs {
		data, _ := organization.(*client.OrganizationEntity)
		organizationID = data.ID
	}

	workspaces := c.list(ctx, fmt.Sprintf("%s/api/v1/organization/%s/workspace?filter[workspace]=name==%s", c.endpoint, organizationID, workspace), new(client.WorkspaceEntity), diags)
	if diags.HasError() {
		return outputs, false
	}
	if len(workspaces) == 0 {
		diags.AddError(fmt.Sprintf("Workspace %q not found!", workspace), fmt.Sprintf("%q", workspace))
		return outputs, false
	}
	var workspaceID string
	for _, ws := range workspaces {
		data, _ := ws.(*client.WorkspaceEntity)
		workspaceID = data.ID
	}

	histories := c.list(ctx, fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/history?sort=-createdDate", c.endpoint, organizationID, workspaceID), new(client.HistoryEntity), diags)
	if diags.HasError() {
		return outputs, false
	}
	if len(histories) == 0 {
		tflog.Info(ctx, "No history information found")
		return outputs, false
	}

	history, _ := histories[0].(*client.HistoryEntity)
	return c.download(ctx, history.Output, diags)
}

// download fetches the output.json file a history entry links to and converts
// its values.outputs into dynamic values.
func (c workspaceOutputsClient) download(ctx context.Context, outputURL string, diags *diag.Diagnostics) (outputs workspaceOutputs, found bool) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, outputURL, nil)
	if err != nil {
		diags.AddError("Error creating request", fmt.Sprintf("Error creating request: %s", err))
		return outputs, false
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")

	response, err := c.client.Do(request)
	if err != nil {
		diags.AddError("Error downloading workspace output", fmt.Sprintf("Error downloading workspace output: %s", err))
		return outputs, false
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading workspace output, response status: %s, error: %s", response.Status, err))
	}
	if reportAPIErrorStatus(diags, "downloading workspace output", response, body) {
		return outputs, false
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		tflog.Error(ctx, "Error converting json result")
		return outputs, false
	}
	values, ok := result["values"].(map[string]any)
	if !ok {
		tflog.Error(ctx, "Error converting values from json result")
		return outputs, false
	}
	rawOutputs, ok := values["outputs"].(map[string]any)
	if !ok {
		tflog.Error(ctx, "Error converting values.outputs from json result")
		return outputs, false
	}

	return convertWorkspaceOutputs(ctx, rawOutputs, diags)
}

func convertWorkspaceOutputs(ctx context.Context, rawOutputs map[string]any, diags *diag.Diagnostics) (outputs workspaceOutputs, found bool) {
	allTypes := map[string]attr.Type{}
	allValues := map[string]attr.Value{}
	nonSensitiveTypes := map[string]attr.Type{}
	nonSensitiveValues := map[string]attr.Value{}

	for name := range rawOutputs {
		output, ok := rawOutputs[name].(map[string]any)
		if !ok {
			tflog.Error(ctx, "Error converting values.outputs.xx from json result")
			return outputs, false
		}
		attrType, _ := inferAttrType(output["value"])
		attrValue, _ := convertToAttrValue(output["value"], attrType)

		allTypes[name] = attrType
		allValues[name] = attrValue
		if output["sensitive"] == false {
			nonSensitiveTypes[name] = attrType
			nonSensitiveValues[name] = attrValue
		}
	}

	all, d := types.ObjectValue(allTypes, allValues)
	diags.Append(d...)
	if diags.HasError() {
		return outputs, false
	}
	nonSensitive, d := types.ObjectValue(nonSensitiveTypes, nonSensitiveValues)
	diags.Append(d...)
	if diags.HasError() {
		return outputs, false
	}

	return workspaceOutputs{
		values:       types.DynamicValue(all),
		nonSensitive: types.DynamicValue(nonSensitive),
	}, true
}

// list runs a JSON:API GET and unmarshals the collection into structType.
func (c workspaceOutputsClient) list(ctx context.Context, url string, structType any, diags *diag.Diagnostics) []any {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		diags.AddError("Error creating Output request", fmt.Sprintf("Error creating Output request: %s", err))
		return nil
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")

	response, err := c.client.Do(request)
	if err != nil {
		diags.AddError("Error executing Output request", fmt.Sprintf("Error executing Output request: %s", err))
		return nil
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading Output response, response status: %s, error: %s", response.Status, err))
	}
	if reportAPIErrorStatus(diags, "reading workspace output", response, body) {
		return nil
	}

	data, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(structType))
	if err != nil {
		diags.AddError("Unable to unmarshal payload", fmt.Sprintf("Unable to unmarshal payload, response status: %s, response body: %s, error: %s", response.Status, string(body), err))
		return nil
	}
	return data
}
