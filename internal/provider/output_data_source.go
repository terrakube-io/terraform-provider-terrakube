package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"reflect"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	terrakube "github.com/terrakube-io/terrakube-go"
)

var (
	_ datasource.DataSource              = &OutputDataSource{}
	_ datasource.DataSourceWithConfigure = &OutputDataSource{}
)

type OutputDataSource struct {
	client *terrakube.Client
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

func (d *OutputDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Output Data Source")
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

	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Info(ctx, state.Workspace.ValueString())
	tflog.Info(ctx, state.Organization.ValueString())

	orgs, err := d.client.Organizations.List(ctx, &terrakube.ListOptions{Filter: "name==" + rsqlQuote(state.Organization.ValueString())})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace output", err)
		return
	}
	organization := findSingleByNameIn(&resp.Diagnostics, "organization", state.Organization.ValueString(), "", orgs,
		func(e *terrakube.Organization) string { return e.Name })
	if organization == nil {
		return
	}
	OrganizationID := organization.ID

	// now try to find the Workspace
	workspaces, err := d.client.Workspaces.List(ctx, OrganizationID, &terrakube.ListOptions{Filter: "name==" + rsqlQuote(state.Workspace.ValueString())})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace output", err)
		return
	}
	workspace := findSingleByNameIn(&resp.Diagnostics, "workspace", state.Workspace.ValueString(), " in organization "+state.Organization.ValueString(), workspaces,
		func(e *terrakube.Workspace) string { return e.Name })
	if workspace == nil {
		return
	}
	WorkspaceId := workspace.ID
	tflog.Info(ctx, WorkspaceId)

	// Now that we found the workspace id we can query for the history
	Histories, err := d.client.History.List(ctx, OrganizationID, WorkspaceId, &terrakube.ListOptions{Sort: "-createdDate"})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace output", err)
		return
	}

	if len(Histories) == 0 {
		// No history for this workspace. That is not an error
		tflog.Info(ctx, "No history information found")
		return
	}

	// Output of the newest history entry names the JSON state file, which contains the real data
	// we need. The token is only sent to the configured endpoint.
	file, err := d.client.State.Download(ctx, Histories[0], terrakube.StateJSON)
	// Terrakube answers a missing output file with 200 and an empty body, because the history entry
	// is saved before the state is uploaded. That is no outputs yet, not a corrupt file.
	if errors.Is(err, terrakube.ErrStateNotFound) {
		tflog.Info(ctx, "The output file is empty, so there are no outputs yet")
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "downloading workspace output", err)
		return
	}
	defer file.Close()

	bodyFile, err := io.ReadAll(file)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading Output response part 4, error: %s", err))
	}

	if len(bytes.TrimSpace(bodyFile)) == 0 {
		tflog.Info(ctx, "The output file is empty, so there are no outputs yet")
		return
	}

	var result map[string]interface{}
	err = json.Unmarshal(bodyFile, &result)
	if err != nil {
		tflog.Error(ctx, "Error converting json result")
		resp.Diagnostics.AddError("Unable to read the workspace output file", fmt.Sprintf("The output file is not valid JSON: %s", err))
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

	// walk through json outputs
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
