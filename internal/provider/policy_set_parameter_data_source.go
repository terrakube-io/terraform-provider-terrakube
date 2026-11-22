package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	terrakube "github.com/terrakube-io/terrakube-go"
)

var (
	_ datasource.DataSource              = &PolicySetParameterDataSource{}
	_ datasource.DataSourceWithConfigure = &PolicySetParameterDataSource{}
)

type PolicySetParameterDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	PolicySetId types.String `tfsdk:"policy_set_id"`
	Key         types.String `tfsdk:"key"`
	Value       types.String `tfsdk:"value"`
	Description types.String `tfsdk:"description"`
}

type PolicySetParameterDataSource struct {
	client *terrakube.Client
}

func NewPolicySetParameterDataSource() datasource.DataSource {
	return &PolicySetParameterDataSource{}
}

func (d *PolicySetParameterDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_policy_set_parameter"
}

func (d *PolicySetParameterDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Query an OPA Policy Set Parameter in Terrakube by policy set ID and key (or ID).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Parameter ID (UUID). Specify either id or key.",
			},
			"policy_set_id": schema.StringAttribute{
				Required:    true,
				Description: "Target Policy Set ID",
			},
			"key": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Parameter key identifier",
			},
			"value": schema.StringAttribute{
				Computed:    true,
				Description: "Parameter value",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Parameter description",
			},
		},
	}
}

func (d *PolicySetParameterDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Policy Set Parameter Data Source")
}

func (d *PolicySetParameterDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state PolicySetParameterDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasID := !state.ID.IsNull() && state.ID.ValueString() != ""
	hasKey := !state.Key.IsNull() && state.Key.ValueString() != ""

	if !hasID && !hasKey {
		resp.Diagnostics.AddError(
			"Missing Search Criteria",
			"At least one of 'id' or 'key' must be specified to query a policy set parameter.",
		)
		return
	}

	policySetID := state.PolicySetId.ValueString()
	var targetParam *terrakube.PolicySetParameter

	if hasID {
		paramID := state.ID.ValueString()
		param, err := d.client.PolicySetParameters.GetByPolicySet(ctx, policySetID, paramID)
		if err != nil {
			var apiErr *terrakube.APIError
			if errors.As(err, &apiErr) {
				resp.Diagnostics.AddError("Policy Set Parameter Not Found", fmt.Sprintf("Parameter ID %s was not found for policy set %s: %s", paramID, policySetID, string(apiErr.Body)))
				return
			}
			reportClientError(&resp.Diagnostics, "reading policy set parameter", err)
			return
		}
		targetParam = param
	} else {
		targetKey := state.Key.ValueString()
		params, err := d.client.PolicySetParameters.ListByPolicySet(ctx, policySetID, nil)
		if err != nil {
			reportClientError(&resp.Diagnostics, "querying policy set parameters", err)
			return
		}

		for _, p := range params {
			if p != nil && stringOrEmpty(p.Key) == targetKey {
				targetParam = p
				break
			}
		}

		if targetParam == nil {
			resp.Diagnostics.AddError(
				"Policy Set Parameter Not Found",
				fmt.Sprintf("No policy set parameter with key %q was found on policy set %q.", targetKey, policySetID),
			)
			return
		}
	}

	state.ID = types.StringValue(targetParam.ID)
	state.Key = types.StringValue(stringOrEmpty(targetParam.Key))
	state.Value = types.StringValue(stringOrEmpty(targetParam.Value))
	state.Description = types.StringPointerValue(targetParam.Description)

	tflog.Debug(ctx, "Policy set parameter data source resolved", map[string]any{"id": targetParam.ID, "key": stringOrEmpty(targetParam.Key)})
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
