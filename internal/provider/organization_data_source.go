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
	_ datasource.DataSource              = &OrganizationDataSource{}
	_ datasource.DataSourceWithConfigure = &OrganizationDataSource{}
)

type OrganizationDataSourceModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Description   types.String `tfsdk:"description"`
	ExecutionMode types.String `tfsdk:"execution_mode"`
	Icon          types.String `tfsdk:"icon"`
}

type OrganizationDataSource struct {
	client   *http.Client
	endpoint string
	token    string
}

func NewOrganizationDataSource() datasource.DataSource {
	return &OrganizationDataSource{}
}

func (d *OrganizationDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, res *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		res.Diagnostics.AddError(
			"Unexpected Organization Data Source Configure Type",
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
	tflog.Info(ctx, "Creating Organization datasource")
}

func (d *OrganizationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (d *OrganizationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Organization Id",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Organization Name",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Organization description information",
			},
			"execution_mode": schema.StringAttribute{
				Computed:    true,
				Description: "Select default execution mode for the organization (remote or local)",
			},
			"icon": schema.StringAttribute{
				Computed:    true,
				Description: "Organization icon in format name:color",
			},
		},
	}
}

func (d *OrganizationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state OrganizationDataSourceModel

	req.Config.Get(ctx, &state)

	reqOrg, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization?filter[organization]=name==%s", d.endpoint, state.Name.ValueString()), nil)
	reqOrg.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	reqOrg.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		tflog.Error(ctx, "Error creating organization datasource request")
	}

	resOrg, err := d.client.Do(reqOrg)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error executing organization datasource request, response status: %s, response body: %s, error: %s", resOrg.Status, resOrg.Body, err))
	}

	body, err := io.ReadAll(resOrg.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading organization response, response status: %s, response body: %s, error: %s", resOrg.Status, resOrg.Body, err))
	}

	var orgs []interface{}

	orgs, err = jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(new(client.OrganizationEntity)))

	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal payload", fmt.Sprintf("Unable to marshal payload, response status: %s, response body: %s, error: %s", resOrg.Status, resOrg.Body, err))
		return
	}

	for _, organization := range orgs {
		data, _ := organization.(*client.OrganizationEntity)
		state.ID = types.StringValue(data.ID)
		state.Name = types.StringValue(data.Name)
		state.Description = types.StringValue(data.Description)
		state.ExecutionMode = types.StringValue(data.ExecutionMode)
		if data.Icon != nil {
			state.Icon = types.StringValue(*data.Icon)
		} else {
			state.Icon = types.StringNull()
		}
	}

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}
