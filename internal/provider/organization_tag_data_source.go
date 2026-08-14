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
	_ datasource.DataSource              = &OrganizationTagDataSource{}
	_ datasource.DataSourceWithConfigure = &OrganizationTagDataSource{}
)

type OrganizationTagDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	OrganizationId types.String `tfsdk:"organization_id"`
}

type OrganizationTagDataSource struct {
	client   *http.Client
	endpoint string
	token    string
}

func NewOrganizationTagDataSource() datasource.DataSource {
	return &OrganizationTagDataSource{}
}

func (d *OrganizationTagDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, res *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		res.Diagnostics.AddError(
			"Unexpected OrganizationTag Data Source Configure Type",
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
	tflog.Info(ctx, "OrganizationTag datasource configured")
}

func (d *OrganizationTagDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_tag"
}

func (d *OrganizationTagDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The ID of the tag",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "The ID of the organization",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "The name of the tag",
			},
		},
	}
}

func (d *OrganizationTagDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state OrganizationTagDataSourceModel

	req.Config.Get(ctx, &state)

	reqOrgTag, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/tag?filter[tag]=name==%s", d.endpoint, state.OrganizationId.ValueString(), state.Name.ValueString()), nil)
	reqOrgTag.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	reqOrgTag.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error creating organization tag datasource request, error: %s", err))
	}

	resOrgTag, err := d.client.Do(reqOrgTag)
	if err != nil {
		resp.Diagnostics.AddError("Error executing organization tag datasource request", fmt.Sprintf("Error executing organization tag datasource request: %s", err))
		return
	}

	body, err := io.ReadAll(resOrgTag.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading organization tag datasource request, error: %s, response status: %s", err, resOrgTag.Status))
	}

	var organizationTags []interface{}

	organizationTags, err = jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(new(client.OrganizationTagEntity)))

	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal payload", fmt.Sprintf("Unable to marshal payload, error: %s, response body: %s", err, body))
		return
	}

	for _, organization := range organizationTags {
		data, _ := organization.(*client.OrganizationTagEntity)
		state.ID = types.StringValue(data.ID)
		state.Name = types.StringValue(data.Name)
	}

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}
