package provider

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"reflect"
	"strings"
	"terraform-provider-terrakube/internal/client"

	"github.com/google/jsonapi"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ datasource.DataSource              = &OutputDataSource{}
	_ datasource.DataSourceWithConfigure = &OutputDataSource{}
)

type OutputDataSource struct {
	client   *http.Client
	endpoint string
	token    string
}

type OutputDataSourceModel struct {
	Organization       types.String  `tfsdk:"organization"`
	Workspace          types.String  `tfsdk:"workspace"`
	Values             types.Dynamic `tfsdk:"values"`
	NonSensitiveValues types.Dynamic `tfsdk:"nonsensitive_values"`
}

func NewOutputDataSource() datasource.DataSource {
	return &OutputDataSource{}
}

func (d *OutputDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, res *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		res.Diagnostics.AddError(
			"Unexpected Output Data Source Configure Type",
			fmt.Sprintf("Expected *TerrakubeConnectionData got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	if providerData.InsecureHttpClient {
		if custom, ok := http.DefaultTransport.(*http.Transport); ok {
			customTransport := custom.Clone()
			customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
			d.client = &http.Client{Transport: customTransport}
		} else {
			d.client = &http.Client{}
		}
	} else {
		d.client = &http.Client{}
	}
	d.endpoint = providerData.Endpoint
	d.token = providerData.Token

	ctx = tflog.SetField(ctx, "endpoint", d.endpoint)
	ctx = tflog.SetField(ctx, "token", d.token)
	ctx = tflog.MaskFieldValuesWithFieldKeys(ctx, "token")
	tflog.Info(ctx, "Creating Output datasource")
}

func (d *OutputDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_output"
}

func (d *OutputDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"workspace": schema.StringAttribute{
				Required:    true,
				Description: "Workspace Name",
			},
			"organization": schema.StringAttribute{
				Required:    true,
				Description: "Organization Name",
			},
			"values": schema.DynamicAttribute{
				Description: `Values of the workspace outputs.`,
				Computed:    true,
				Sensitive:   true,
			},
			"nonsensitive_values": schema.DynamicAttribute{
				Description: `Non-sensitive values of the workspace outputs.`,
				Computed:    true,
			},
		},
	}
}

func (d *OutputDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state OutputDataSourceModel

	req.Config.Get(ctx, &state)
	tflog.Info(ctx, state.Workspace.ValueString())
	tflog.Info(ctx, state.Organization.ValueString())

	orgs := d.ReadDataFromApi(fmt.Sprintf("%s/api/v1/organization?filter[organization]=name==%s", d.endpoint, state.Organization.ValueString()), ctx, resp, new(client.OrganizationEntity))

	if len(orgs) == 0 {
		resp.Diagnostics.AddError(fmt.Sprintf("Organization %s not found!", state.Organization.String()), state.Organization.String())
		return
	}

	var OrganizationID string
	for _, organization := range orgs {
		data, _ := organization.(*client.OrganizationEntity)
		OrganizationID = data.ID
	}

	//now try to find the Workspace
	workspaces := d.ReadDataFromApi(fmt.Sprintf("%s/api/v1/organization/%s/workspace?filter[workspace]=name==%s", d.endpoint, OrganizationID, state.Workspace.ValueString()), ctx, resp, new(client.WorkspaceEntity))

	if len(workspaces) == 0 {
		resp.Diagnostics.AddError(fmt.Sprintf("Workspace %s not found!", state.Workspace.String()), state.Workspace.String())
		return
	}

	var WorkspaceId string
	for _, ws := range workspaces {
		data, _ := ws.(*client.WorkspaceEntity)
		WorkspaceId = data.ID
	}
	tflog.Info(ctx, WorkspaceId)

	//Now that we found the worspace id we can query for the history
	Histories := d.ReadDataFromApi(fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/history?sort=-createdDate", d.endpoint, OrganizationID, WorkspaceId), ctx, resp, new(client.HistoryEntity))

	if len(Histories) == 0 {
		//No history for this workspace. That is not an error
		tflog.Info(ctx, "No history information found")
		return
	}

	data, _ := Histories[0].(*client.HistoryEntity)
	tflog.Info(ctx, fmt.Sprintf("%#v", data))
	//Output contains a link to the Output.json file, which contains the real data we need.
	OutputUrl := data.Output
	reqFile, err := http.NewRequest(http.MethodGet, OutputUrl, nil)
	reqFile.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	reqFile.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error creating Output datasource request for output json file failed (%s)", OutputUrl))
	}

	resFile, err := d.client.Do(reqFile)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error executing Output datasource request part 4, response status: %s, response body: %s, error: %s", resFile.Status, resFile.Body, err))
	}

	bodyFile, err := io.ReadAll(resFile.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading Output response part 4, response status: %s, response body: %s, error: %s", resFile.Status, resFile.Body, err))
	}

	var result map[string]interface{}
	err = json.Unmarshal(bodyFile, &result)
	if err != nil {
		tflog.Error(ctx, "Error converting json result")
		return
	}

	values, test := result["values"].(map[string]interface{})
	if !test {
		tflog.Error(ctx, "Error converting values from json result")
		return
	}
	outputs, test := values["outputs"].(map[string]interface{})
	if !test {
		tflog.Error(ctx, "Error converting values.outputs from json result")
		return
	}

	sensitiveTypes := map[string]attr.Type{}
	sensitiveValues := map[string]attr.Value{}
	nonSensitiveTypes := map[string]attr.Type{}
	nonSensitiveValues := map[string]attr.Value{}

	//walk trhough json outputs
	for x := range outputs {
		myOutput, test := outputs[x].(map[string]interface{})
		if !test {
			tflog.Error(ctx, "Error converting values.outputs.xx from json result")
			return
		} else {
			attrType, _ := inferAttrType(myOutput["value"])
			attrValue, _ := convertToAttrValue(myOutput["value"], attrType)

			sensitiveTypes[x] = attrType
			sensitiveValues[x] = attrValue

			if myOutput["sensitive"] == false {
				nonSensitiveTypes[x] = attrType
				nonSensitiveValues[x] = attrValue
			}
		}
	}

	// Create dynamic attribute value for `sensitive_values`
	obj, diags := types.ObjectValue(sensitiveTypes, sensitiveValues)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	sensitiveOutputs := types.DynamicValue(obj)

	// Create dynamic attribute value for `nonsensitive_values`
	obj, diags = types.ObjectValue(nonSensitiveTypes, nonSensitiveValues)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	nonSensitiveOutputs := types.DynamicValue(obj)

	state.NonSensitiveValues = nonSensitiveOutputs
	state.Values = sensitiveOutputs

	diags2 := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags2...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func convertToAttrValue(raw interface{}, t attr.Type) (attr.Value, diag.Diagnostics) {
	var diags diag.Diagnostics

	if raw == nil {
		return types.StringNull(), diags
	}

	if t == types.BoolType {
		b, ok := raw.(bool)
		if !ok {
			diags.AddError("Conversion Error", "expected bool")
			return types.BoolNull(), diags
		}
		return types.BoolValue(b), diags
	}

	if t == types.NumberType {
		// Use a float64 conversion to handle all numeric types.
		n, ok := raw.(float64)
		if !ok {
			diags.AddError("Conversion Error", "expected number")
			return types.NumberNull(), diags
		}
		return types.NumberValue(big.NewFloat(n)), diags
	}

	if t == types.StringType {
		s, ok := raw.(string)
		if !ok {
			diags.AddError("Conversion Error", "expected string")
			return types.StringNull(), diags
		}
		return types.StringValue(s), diags
	}

	// For composite types, use a type switch on the expected type.
	switch tt := t.(type) {
	case types.ListType:
		// Expect raw to be a slice.
		slice, ok := raw.([]interface{})
		if !ok {
			diags.AddError("Conversion Error", "expected slice for ListType")
			return types.ListNull(tt.ElemType), diags
		}

		var elems []attr.Value
		for _, elem := range slice {
			v, ds := convertToAttrValue(elem, tt.ElemType)
			diags.Append(ds...)
			elems = append(elems, v)
		}
		return types.ListValue(tt.ElemType, elems)

	case types.TupleType:
		// Expect raw to be a slice.
		slice, ok := raw.([]interface{})
		if !ok {
			diags.AddError("Conversion Error", "expected slice for TupleType")
			return types.TupleNull(tt.ElemTypes), diags
		}
		if len(slice) != len(tt.ElemTypes) {
			diags.AddError("Conversion Error", "tuple length mismatch")
			return types.TupleNull(tt.ElemTypes), diags
		}

		var elems []attr.Value
		for i, elem := range slice {
			v, ds := convertToAttrValue(elem, tt.ElemTypes[i])
			diags.Append(ds...)
			elems = append(elems, v)
		}
		return types.TupleValue(tt.ElemTypes, elems)

	case types.ObjectType:
		// Expect raw to be a map[string]interface{}.
		m, ok := raw.(map[string]interface{})
		if !ok {
			diags.AddError("Conversion Error", "expected map for ObjectType")
			return types.ObjectNull(tt.AttrTypes), diags
		}

		objValues := make(map[string]attr.Value)
		// Iterate over the expected attributes defined in the ObjectType.
		for key, expectedType := range tt.AttrTypes {
			value := m[key]
			v, ds := convertToAttrValue(value, expectedType)
			diags.Append(ds...)
			if ds.HasError() {
				return types.ObjectNull(tt.AttrTypes), diags
			}

			objValues[key] = v
		}
		return types.ObjectValue(tt.AttrTypes, objValues)

	case types.MapType:
		// Expect raw to be a map[string]interface{}.
		m, ok := raw.(map[string]interface{})
		if !ok {
			diags.AddError("Conversion Error", "expected map for MapType")
			return types.MapValue(tt.ElemType, nil)
		}

		mapValues := make(map[string]attr.Value)
		for key, value := range m {
			v, ds := convertToAttrValue(value, tt.ElemType)
			diags.Append(ds...)
			if ds.HasError() {
				return types.MapValue(tt.ElemType, nil)
			}

			mapValues[key] = v
		}
		return types.MapValue(tt.ElemType, mapValues)

	default:
		diags.AddError("Conversion Error", fmt.Sprintf("unsupported type %T", t))
	}

	return nil, diags
}

func inferAttrType(raw interface{}) (attr.Type, error) {
	if raw == nil {
		return types.StringType, nil // nil attribute values will be converted to types.StringNull, so the Type for this value wil be types.StringType
	}

	switch v := raw.(type) {
	case bool:
		return types.BoolType, nil
	case int, int8, int16, int32, int64, float32, float64:
		return types.NumberType, nil
	case string:
		return types.StringType, nil
	case []interface{}:
		// For slices, if the slice is empty return a List with a dynamic element type.
		if len(v) == 0 {
			return types.ListType{ElemType: types.DynamicType}, nil
		}

		// Infer the type for the first element.
		firstType, err := inferAttrType(v[0])
		if err != nil {
			return nil, err
		}

		// Check if all elements are of the same type.
		homogeneous := true
		for i := 1; i < len(v); i++ {
			currType, err := inferAttrType(v[i])
			if err != nil {
				return nil, err
			}
			if !reflect.DeepEqual(firstType, currType) {
				homogeneous = false
				break
			}
		}
		if homogeneous {
			return types.ListType{ElemType: firstType}, nil
		}

		// If not homogeneous, build a Tuple with each element’s inferred type.
		tupleTypes := make([]attr.Type, len(v))
		for i, elem := range v {
			t, err := inferAttrType(elem)
			if err != nil {
				return nil, err
			}
			tupleTypes[i] = t
		}
		return types.TupleType{ElemTypes: tupleTypes}, nil
	case map[string]interface{}:
		// Build an Object type by inferring each attribute's type.
		attrTypes := make(map[string]attr.Type)
		for key, val := range v {
			inferred, err := inferAttrType(val)
			if err != nil {
				return nil, fmt.Errorf("error inferring type for key %q: %w", key, err)
			}
			attrTypes[key] = inferred
		}
		return types.ObjectType{AttrTypes: attrTypes}, nil

	default:
		return nil, fmt.Errorf("unsupported type %T", raw)
	}
}

func (d *OutputDataSource) ReadDataFromApi(url string, ctx context.Context, resp *datasource.ReadResponse, structType any) (data []interface{}) {
	regApi, err := http.NewRequest(http.MethodGet, url, nil)
	regApi.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	regApi.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		tflog.Error(ctx, "Error creating Output datasource request")
	}

	resApi, err := d.client.Do(regApi)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error executing Output datasource request, response status: %s, response body: %s, error: %s", resApi.Status, resApi.Body, err))
	}

	body, err := io.ReadAll(resApi.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading Output response, response status: %s, response body: %s, error: %s", resApi.Status, resApi.Body, err))
	}

	tflog.Info(ctx, string(body))

	data, err = jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(structType))

	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal payload", fmt.Sprintf("Unable to marshal payload, response status: %s, response body: %s, error: %s", resApi.Status, resApi.Body, err))
		return
	}

	return data
}
