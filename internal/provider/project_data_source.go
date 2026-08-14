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
	_ datasource.DataSource              = &ProjectDataSource{}
	_ datasource.DataSourceWithConfigure = &ProjectDataSource{}
)

type ProjectDataSourceModel struct {
	ID           types.String `tfsdk:"id"`
	Organization types.String `tfsdk:"organization"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
}

type ProjectDataSource struct {
	client   *http.Client
	endpoint string
	token    string
}

func NewProjectDataSource() datasource.DataSource {
	return &ProjectDataSource{}
}

func (d *ProjectDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, res *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		res.Diagnostics.AddError(
			"Unexpected Project Data Source Configure Type",
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
	tflog.Info(ctx, "Creating Project datasource")
}

func (d *ProjectDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (d *ProjectDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Project Id",
			},
			"organization": schema.StringAttribute{
				Required:    true,
				Description: "Organization Name",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Project Name",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Project description",
			},
		},
	}
}

func (d *ProjectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state ProjectDataSourceModel

	req.Config.Get(ctx, &state)
	tflog.Info(ctx, fmt.Sprintf("organization : %s", state.Organization.ValueString()))
	tflog.Info(ctx, fmt.Sprintf("project : %s", state.Name.ValueString()))

	projectName := state.Name.ValueString()

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

	projects := d.ReadDataFromApi(fmt.Sprintf("%s/api/v1/organization/%s/project?filter[project]=name==%s", d.endpoint, OrganizationID, projectName), ctx, resp, new(client.ProjectEntity))
	if len(projects) == 0 {
		resp.Diagnostics.AddError(fmt.Sprintf("Project %s not found!", projectName), projectName)
		return
	}

	for _, project := range projects {
		data, _ := project.(*client.ProjectEntity)
		state.ID = types.StringValue(data.ID)
		state.Name = types.StringValue(data.Name)
		state.Description = types.StringPointerValue(data.Description)
	}

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (d *ProjectDataSource) ReadDataFromApi(url string, ctx context.Context, resp *datasource.ReadResponse, structType any) (data []interface{}) {
	regApi, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating Project datasource request", fmt.Sprintf("Error creating Project datasource request: %s", err))
		return
	}
	regApi.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	regApi.Header.Add("Content-Type", "application/vnd.api+json")

	resApi, err := d.client.Do(regApi)
	if err != nil {
		resp.Diagnostics.AddError("Error executing Project datasource request", fmt.Sprintf("Error executing Project datasource request: %s", err))
		return
	}

	body, err := io.ReadAll(resApi.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading Project response, response status: %s, error: %s", resApi.Status, err))
	}

	tflog.Info(ctx, string(body))

	data, err = jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(structType))

	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal payload", fmt.Sprintf("Unable to marshal payload, response status: %s, response body: %s, error: %s", resApi.Status, string(body), err))
		return
	}

	return data
}
