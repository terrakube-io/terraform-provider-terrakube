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
	_ datasource.DataSource              = &VcsDataSource{}
	_ datasource.DataSourceWithConfigure = &VcsDataSource{}
)

type VcsDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	ClientId       types.String `tfsdk:"client_id"`
	Endpoint       types.String `tfsdk:"endpoint"`
	ApiUrl         types.String `tfsdk:"api_url"`
	Status         types.String `tfsdk:"status"`
}

type VcsDataSource struct {
	client   *http.Client
	endpoint string
	token    string
}

func NewVcsDataSource() datasource.DataSource {
	return &VcsDataSource{}
}

func (d *VcsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, res *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		res.Diagnostics.AddError(
			"Unexpected Vcs Data Source Configure Type",
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

	tflog.Info(ctx, "Creating Vcs datasource")
}

func (d *VcsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vcs"
}

func (d *VcsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Vcs Id",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization id",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Vcs Name",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Vcs description information",
			},
			"client_id": schema.StringAttribute{
				Computed:    true,
				Description: "The client id of the Vcs provider",
			},
			"endpoint": schema.StringAttribute{
				Computed:    true,
				Description: "The endpoint of the Vcs provider",
			},
			"api_url": schema.StringAttribute{
				Computed:    true,
				Description: "The api url of the Vcs provider",
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "The status of the Vcs provider",
			},
		},
	}
}

func (d *VcsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state VcsDataSourceModel

	req.Config.Get(ctx, &state)

	apiURL := fmt.Sprintf("%s/api/v1/organization/%s/vcs?filter[vcs]=name=='%s'", d.endpoint, state.OrganizationId.ValueString(), url.PathEscape(state.Name.ValueString()))
	requestVcs, err := http.NewRequest(http.MethodGet, apiURL, nil)
	requestVcs.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	requestVcs.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating ssh request", fmt.Sprintf("Error creating team resource request: %s", err))
		return
	}

	responseVcs, err := d.client.Do(requestVcs)
	if err != nil {
		resp.Diagnostics.AddError("Error executing ssh request", fmt.Sprintf("Error executing ssh request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(responseVcs.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading ssh response body", fmt.Sprintf("Error reading team resource response body: %s", err))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	var vcss []interface{}

	vcss, err = jsonapi.UnmarshalManyPayload(strings.NewReader(string(bodyResponse)), reflect.TypeOf(new(client.VcsEntity)))

	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal payload", fmt.Sprintf("Unable to unmarshal payload, error: %s, response status %s, response body %s", err, responseVcs.Status, string(bodyResponse)))
		return
	}

	for _, vcs := range vcss {
		data, _ := vcs.(*client.VcsEntity)
		state.ID = types.StringValue(data.ID)
		state.Description = types.StringValue(data.Description)
		state.ClientId = types.StringValue(data.ClientId)
		state.Endpoint = types.StringValue(data.Endpoint)
		state.ApiUrl = types.StringValue(data.ApiUrl)
		state.Status = types.StringValue(data.Status)
	}

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}
