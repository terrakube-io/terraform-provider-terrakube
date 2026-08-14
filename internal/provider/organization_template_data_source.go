package provider

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	_ datasource.DataSource              = &OrganizationTemplateDataSource{}
	_ datasource.DataSourceWithConfigure = &OrganizationTemplateDataSource{}
)

type OrganizationTemplateDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	OrganizationId types.String `tfsdk:"organization_id"`
}

type OrganizationTemplateDataSource struct {
	client   *http.Client
	endpoint string
	token    string
}

func NewOrganizationTemplateDataSource() datasource.DataSource {
	return &OrganizationTemplateDataSource{}
}

func (d *OrganizationTemplateDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, res *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		res.Diagnostics.AddError(
			"Unexpected Organization Template Data Source Configure Type",
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
	tflog.Info(ctx, "Organization Template Data Source configured")
}

func (d *OrganizationTemplateDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_template"
}

func (d *OrganizationTemplateDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Id",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Organization Template Name",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Organization ID",
			},
		},
	}
}

func (d *OrganizationTemplateDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state OrganizationTemplateDataSourceModel

	req.Config.Get(ctx, &state)

	apiUrl := fmt.Sprintf("%s/api/v1/organization/%s/template?filter[template]=name=='%s'", d.endpoint, state.OrganizationId.ValueString(), url.PathEscape(state.Name.ValueString()))
	reqTemplate, err := http.NewRequest(http.MethodGet, apiUrl, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating organization template datasource request", fmt.Sprintf("Error creating organization template datasource request: %s", err))
		return
	}
	reqTemplate.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	reqTemplate.Header.Add("Content-Type", "application/vnd.api+json")

	resTemplate, err := d.client.Do(reqTemplate)
	if err != nil {
		resp.Diagnostics.AddError("Error executing organization template datasource request", fmt.Sprintf("Error executing organization template datasource request: %s", err))
		return
	}

	body, err := io.ReadAll(resTemplate.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading organization response")
	}

	var templates []interface{}

	templates, err = jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(new(client.OrganizationTemplateEntity)))

	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal payload", fmt.Sprintf("Unable to marshal, error: %s, response status %s, response body %s", err, resTemplate.Status, string(body)))
		return
	}

	for _, template := range templates {
		data, _ := template.(*client.OrganizationTemplateEntity)
		state.ID = types.StringValue(data.ID)
		state.Name = types.StringValue(data.Name)
	}

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}
