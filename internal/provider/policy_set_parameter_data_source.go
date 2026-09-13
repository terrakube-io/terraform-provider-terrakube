package provider

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"terraform-provider-terrakube/internal/client"

	"github.com/google/jsonapi"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
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
	client   *http.Client
	endpoint string
	token    string
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
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Policy Set Parameter Data Source Configure Type",
			fmt.Sprintf("Expected *TerrakubeConnectionData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
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
	var targetParam *client.PolicySetParameterEntity

	if hasID {
		paramID := state.ID.ValueString()
		url := fmt.Sprintf("%s/api/v1/policy_set/%s/parameters/%s", d.endpoint, policySetID, paramID)
		readReq, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			resp.Diagnostics.AddError("Error creating policy set parameter read request", fmt.Sprintf("Error: %s", err))
			return
		}
		readReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
		readReq.Header.Add("Content-Type", "application/vnd.api+json")

		readResp, err := d.client.Do(readReq)
		if err != nil {
			resp.Diagnostics.AddError("Error executing policy set parameter read request", fmt.Sprintf("Error: %s", err))
			return
		}
		defer readResp.Body.Close()

		bodyResponse, err := io.ReadAll(readResp.Body)
		if err != nil {
			resp.Diagnostics.AddError("Error reading policy set parameter response body", fmt.Sprintf("Error: %s", err))
			return
		}

		if readResp.StatusCode >= http.StatusBadRequest {
			resp.Diagnostics.AddError("Policy Set Parameter Not Found", fmt.Sprintf("Parameter ID %s was not found for policy set %s: %s", paramID, policySetID, string(bodyResponse)))
			return
		}

		param := &client.PolicySetParameterEntity{}
		if err := jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), param); err != nil {
			resp.Diagnostics.AddError("Error unmarshalling policy set parameter payload", fmt.Sprintf("Error: %s", err))
			return
		}
		targetParam = param
	} else {
		targetKey := state.Key.ValueString()
		url := fmt.Sprintf("%s/api/v1/policy_set/%s/parameters", d.endpoint, policySetID)
		readReq, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			resp.Diagnostics.AddError("Error creating policy set parameter query request", fmt.Sprintf("Error: %s", err))
			return
		}
		readReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
		readReq.Header.Add("Content-Type", "application/vnd.api+json")

		readResp, err := d.client.Do(readReq)
		if err != nil {
			resp.Diagnostics.AddError("Error executing policy set parameter query request", fmt.Sprintf("Error: %s", err))
			return
		}
		defer readResp.Body.Close()

		bodyResponse, err := io.ReadAll(readResp.Body)
		if err != nil {
			resp.Diagnostics.AddError("Error reading policy set parameter list response", fmt.Sprintf("Error: %s", err))
			return
		}

		if readResp.StatusCode >= http.StatusBadRequest {
			resp.Diagnostics.AddError("Error querying policy set parameters", fmt.Sprintf("status: %v, body: %v", readResp.Status, string(bodyResponse)))
			return
		}

		rawParams, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(bodyResponse)), reflect.TypeOf(new(client.PolicySetParameterEntity)))
		if err != nil {
			resp.Diagnostics.AddError("Error unmarshalling policy set parameters", fmt.Sprintf("Error: %s", err))
			return
		}

		for _, item := range rawParams {
			p, ok := item.(*client.PolicySetParameterEntity)
			if ok && p.Key == targetKey {
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
	state.Key = types.StringValue(targetParam.Key)
	state.Value = types.StringValue(targetParam.Value)
	if targetParam.Description != nil {
		state.Description = types.StringValue(*targetParam.Description)
	} else {
		state.Description = types.StringNull()
	}

	tflog.Debug(ctx, "Policy set parameter data source resolved", map[string]any{"id": targetParam.ID, "key": targetParam.Key})
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
