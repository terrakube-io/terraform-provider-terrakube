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
	_ datasource.DataSource              = &TeamDataSource{}
	_ datasource.DataSourceWithConfigure = &TeamDataSource{}
)

type TeamDataSourceModel struct {
	Role             types.String `tfsdk:"role"`
	PlanJob          types.Bool   `tfsdk:"plan_job"`
	ApproveJob       types.Bool   `tfsdk:"approve_job"`
	DestroyJob       types.Bool   `tfsdk:"destroy_job"`
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
	ManagePolicies   types.Bool   `tfsdk:"manage_policies"`
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
			"role":        schema.StringAttribute{Computed: true, Description: "Predefined RBAC role"},
			"plan_job":    schema.BoolAttribute{Computed: true, Description: "Allow queuing plans"},
			"approve_job": schema.BoolAttribute{Computed: true, Description: "Allow approving/applying runs"},
			"destroy_job": schema.BoolAttribute{Computed: true, Description: "Allow destroy runs for custom roles (Terrakube 2.34.0+)"},
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
			"manage_policies": schema.BoolAttribute{
				Computed:    true,
				Description: "Allow managing OPA policy sets, attachments, and exemptions",
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

	orgs := d.ReadDataFromApi(fmt.Sprintf("%s/api/v1/organization?filter[organization]=name==%s", d.endpoint, url.QueryEscape(rsqlQuote(state.Organization.ValueString()))), ctx, resp, new(client.OrganizationEntity))

	if resp.Diagnostics.HasError() {
		return
	}
	organization := findSingleByName(&resp.Diagnostics, "organization", state.Organization.ValueString(), "", orgs,
		func(e *client.OrganizationEntity) string { return e.Name })
	if organization == nil {
		return
	}
	OrganizationID := organization.ID

	teams := d.ReadDataFromApi(fmt.Sprintf("%s/api/v1/organization/%s/team?filter[team]=name==%s", d.endpoint, url.PathEscape(OrganizationID), url.QueryEscape(rsqlQuote(teamName))), ctx, resp, new(client.TeamEntity))
	if resp.Diagnostics.HasError() {
		return
	}
	data := findSingleByName(&resp.Diagnostics, "team", teamName, " in organization "+state.Organization.ValueString(), teams,
		func(e *client.TeamEntity) string { return e.Name })
	if data == nil {
		return
	}
	state.Role = roleToState(data.Role)
	state.PlanJob = types.BoolValue(data.PlanJob)
	state.ApproveJob = types.BoolValue(data.ApproveJob)
	state.DestroyJob = types.BoolPointerValue(data.DestroyJob)
	state.ManageCollection = types.BoolValue(data.ManageCollection)
	state.ManageJob = types.BoolValue(data.ManageJob)
	state.ManageModule = types.BoolValue(data.ManageModule)
	state.ManageProvider = types.BoolValue(data.ManageProvider)
	state.ManageState = types.BoolValue(data.ManageState)
	state.ManageTemplate = types.BoolValue(data.ManageTemplate)
	state.ManageVcs = types.BoolValue(data.ManageVcs)
	state.ManageWorkspace = types.BoolValue(data.ManageWorkspace)
	state.ManagePolicies = types.BoolPointerValue(data.ManagePolicies)

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (d *TeamDataSource) ReadDataFromApi(url string, ctx context.Context, resp *datasource.ReadResponse, structType any) (data []interface{}) {
	regApi, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating Team datasource request", fmt.Sprintf("Error creating Team datasource request: %s", err))
		return
	}
	regApi.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	regApi.Header.Add("Content-Type", "application/vnd.api+json")

	resApi, err := d.client.Do(regApi)
	if err != nil {
		resp.Diagnostics.AddError("Error executing Team datasource request", fmt.Sprintf("Error executing Team datasource request: %s", err))
		return
	}

	body, err := io.ReadAll(resApi.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading Team response, response status: %s, error: %s", resApi.Status, err))
	}

	tflog.Info(ctx, string(body))

	if reportAPIErrorStatus(&resp.Diagnostics, "reading team", resApi, body) {
		return
	}

	data, err = jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(structType))

	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal payload", fmt.Sprintf("Unable to marshal payload, response status: %s, response body: %s, error: %s", resApi.Status, string(body), err))
		return
	}

	return data
}
