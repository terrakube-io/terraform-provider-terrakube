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
	_ datasource.DataSource              = &TeamDataSource{}
	_ datasource.DataSourceWithConfigure = &TeamDataSource{}
)

type TeamDataSourceModel struct {
	Organization     types.String `tfsdk:"organization"`
	Name             types.String `tfsdk:"name"`
	ManageCollection types.Bool   `tfsdk:"manage_collection"`
	ManageJob        types.Bool   `tfsdk:"manage_job"`
	ManageModule     types.Bool   `tfsdk:"manage_module"`
	ManageProvider   types.Bool   `tfsdk:"manage_provider"`
	ManageState      types.Bool   `tfsdk:"manage_state"`
	ManageTemplate   types.Bool   `tfsdk:"manage_template"`
	ManageVcs        types.Bool   `tfsdk:"manage_vcs"`
	ManageWorkspace  types.Bool   `tfsdk:"manage_workspace"`
}

type TeamDataSource struct {
	client   *http.Client
	endpoint string
	token    string
}

func NewTeamDataSource() datasource.DataSource {
	return &TeamDataSource{}
}

func (d *TeamDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, res *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		res.Diagnostics.AddError(
			"Unexpected Team Data Source Configure Type",
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
	tflog.Info(ctx, "Creating Team datasource")
}

func (d *TeamDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_team"
}

func (d *TeamDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"organization": schema.StringAttribute{
				Required:    true,
				Description: "Organization Name",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Team Name",
			},
			"manage_collection": schema.BoolAttribute{
				Computed:    true,
				Description: "Manages collection",
			},
			"manage_job": schema.BoolAttribute{
				Computed:    true,
				Description: "Manage Jobs",
			},
			"manage_module": schema.BoolAttribute{
				Computed:    true,
				Description: "Manage modules",
			},
			"manage_provider": schema.BoolAttribute{
				Computed:    true,
				Description: "Manage providers",
			},
			"manage_state": schema.BoolAttribute{
				Computed:    true,
				Description: "Manage states",
			},
			"manage_template": schema.BoolAttribute{
				Computed:    true,
				Description: "Manage templatess",
			},
			"manage_vcs": schema.BoolAttribute{
				Computed:    true,
				Description: "Manage vcs",
			},
			"manage_workspace": schema.BoolAttribute{
				Computed:    true,
				Description: "Manage workspaces",
			},
		},
	}
}

func (d *TeamDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state TeamDataSourceModel

	req.Config.Get(ctx, &state)
	tflog.Info(ctx, fmt.Sprintf("organization : %s", state.Organization.ValueString()))
	tflog.Info(ctx, fmt.Sprintf("team : %s", state.Name.ValueString()))

	teamName := state.Name.ValueString()

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

	teams := d.ReadDataFromApi(fmt.Sprintf("%s/api/v1/organization/%s/team?filter[team]=name==%s", d.endpoint, OrganizationID, teamName), ctx, resp, new(client.TeamEntity))
	if len(teams) == 0 {
		resp.Diagnostics.AddError(fmt.Sprintf("Team %s not found!", teamName), teamName)
		return
	}

	for _, team := range teams {
		data, _ := team.(*client.TeamEntity)
		state.ManageCollection = types.BoolValue(data.ManageCollection)
		state.ManageJob = types.BoolValue(data.ManageJob)
		state.ManageModule = types.BoolValue(data.ManageModule)
		state.ManageProvider = types.BoolValue(data.ManageProvider)
		state.ManageState = types.BoolValue(data.ManageState)
		state.ManageTemplate = types.BoolValue(data.ManageTemplate)
		state.ManageVcs = types.BoolValue(data.ManageVcs)
		state.ManageWorkspace = types.BoolValue(data.ManageWorkspace)
	}

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (d *TeamDataSource) ReadDataFromApi(url string, ctx context.Context, resp *datasource.ReadResponse, structType any) (data []interface{}) {
	regApi, err := http.NewRequest(http.MethodGet, url, nil)
	regApi.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	regApi.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		tflog.Error(ctx, "Error creating Team datasource request")
	}

	resApi, err := d.client.Do(regApi)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error executing Team datasource request, response status: %s, response body: %s, error: %s", resApi.Status, resApi.Body, err))
	}

	body, err := io.ReadAll(resApi.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading Team response, response status: %s, response body: %s, error: %s", resApi.Status, resApi.Body, err))
	}

	tflog.Info(ctx, string(body))

	data, err = jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(structType))

	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal payload", fmt.Sprintf("Unable to marshal payload, response status: %s, response body: %s, error: %s", resApi.Status, resApi.Body, err))
		return
	}

	return data
}
