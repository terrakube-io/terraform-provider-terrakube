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
	_ datasource.DataSource              = &WorkspaceDataSource{}
	_ datasource.DataSourceWithConfigure = &WorkspaceDataSource{}
)

type WorkspaceDataSource struct {
	client   *http.Client
	endpoint string
	token    string
}

type WorkspaceDataSourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Description      types.String `tfsdk:"description"`
	Organization     types.String `tfsdk:"organization"`
	OrganizationID   types.String `tfsdk:"organization_id"`
	Source           types.String `tfsdk:"source"`
	Branch           types.String `tfsdk:"branch"`
	Folder           types.String `tfsdk:"folder"`
	TemplateID       types.String `tfsdk:"template_id"`
	IaCType          types.String `tfsdk:"iactype"`
	IaCVersion       types.String `tfsdk:"iacversion"`
	ExecutionMode    types.String `tfsdk:"executionmode"`
	Deleted          types.Bool   `tfsdk:"deleted"`
	AllowRemoteApply types.Bool   `tfsdk:"allowremoteapply"`
	VCSID            types.String `tfsdk:"vcsid"`
}

func NewWorkspaceDataSource() datasource.DataSource {
	return &WorkspaceDataSource{}
}

func (d *WorkspaceDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, res *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		res.Diagnostics.AddError(
			"Unexpected Workspace Data Source Configure Type",
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
	tflog.Info(ctx, "Creating Workspace datasource")
}

func (d *WorkspaceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

func (d *WorkspaceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Workspace Id",
				Computed:    true,
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Workspace Name",
			},
			"organization": schema.StringAttribute{
				Required:    true,
				Description: "Organization Name",
			},
			"description": schema.StringAttribute{
				Description: "Workspace description information",
				Computed:    true,
			},
			"organization_id": schema.StringAttribute{
				Description: "organization ID",
				Computed:    true,
			},
			"source": schema.StringAttribute{
				Description: "Source",
				Computed:    true,
			},
			"branch": schema.StringAttribute{
				Description: "Branch",
				Computed:    true,
			},
			"folder": schema.StringAttribute{
				Description: "Folder",
				Computed:    true,
			},
			"template_id": schema.StringAttribute{
				Description: "template ID",
				Computed:    true,
			},
			"iactype": schema.StringAttribute{
				Description: "IaC type",
				Computed:    true,
			},
			"iacversion": schema.StringAttribute{
				Description: "IaC version",
				Computed:    true,
			},
			"executionmode": schema.StringAttribute{
				Description: "Execution mode",
				Computed:    true,
			},
			"deleted": schema.BoolAttribute{
				Description: "Deleted",
				Computed:    true,
			},
			"allowremoteapply": schema.BoolAttribute{
				Description: "Allow remote apply",
				Computed:    true,
			},
			"vcsid": schema.StringAttribute{
				Description: "VCS ID",
				Computed:    true,
			},
		},
	}
}

func (d *WorkspaceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state WorkspaceDataSourceModel

	req.Config.Get(ctx, &state)

	reqOrg, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization?filter[organization]=name==%s", d.endpoint, state.Organization.ValueString()), nil)
	reqOrg.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	reqOrg.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		tflog.Error(ctx, "Error creating Workspace datasource request")
	}

	resOrg, err := d.client.Do(reqOrg)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error executing Workspace datasource request, response status: %s, response body: %s, error: %s", resOrg.Status, resOrg.Body, err))
	}

	body, err := io.ReadAll(resOrg.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading Workspace response, response status: %s, response body: %s, error: %s", resOrg.Status, resOrg.Body, err))
	}

	tflog.Info(ctx, string(body))
	var orgs []interface{}

	orgs, err = jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(new(client.OrganizationEntity)))

	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal payload", fmt.Sprintf("Unable to marshal payload, response status: %s, response body: %s, error: %s", resOrg.Status, resOrg.Body, err))
		return
	}

	if len(orgs) == 0 {
		resp.Diagnostics.AddError(fmt.Sprintf("Organization %s not found!", state.Organization.String()), state.Organization.String())
		return
	}

	for _, organization := range orgs {
		data, _ := organization.(*client.OrganizationEntity)
		state.OrganizationID = types.StringValue(data.ID)
		state.ID = types.StringValue(data.ID)
		state.Description = types.StringValue(data.Description)
	}

	//now try to find the workspace
	reqWS, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/workspace?filter[workspace]=name==%s", d.endpoint, state.OrganizationID.ValueString(), state.Name.ValueString()), nil)
	reqWS.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	reqWS.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		tflog.Error(ctx, "Error creating Workspace datasource request part 2")
	}

	resWS, err := d.client.Do(reqWS)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error executing Workspace datasource request part 2, response status: %s, response body: %s, error: %s", resWS.Status, resWS.Body, err))
	}

	bodyws, errws := io.ReadAll(resWS.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading Workspace response part 2, response status: %s, response body: %s, error: %s", resWS.Status, resWS.Body, errws))
	}

	tflog.Info(ctx, string(bodyws))
	var workspaces []interface{}

	workspaces, err = jsonapi.UnmarshalManyPayload(strings.NewReader(string(bodyws)), reflect.TypeOf(new(client.WorkspaceEntity)))

	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal payload", fmt.Sprintf("Unable to marshal payload, response status: %s, response body: %s, error: %s", resWS.Status, resWS.Body, err))
		return
	}

	if len(workspaces) == 0 {
		resp.Diagnostics.AddError(fmt.Sprintf("Workspace %s not found!", state.Name.String()), state.Name.String())
		return
	}

	for _, workspace := range workspaces {
		data, _ := workspace.(*client.WorkspaceEntity)
		state.ID = types.StringValue(data.ID)
		state.Description = types.StringValue(data.Description)
		state.Source = types.StringValue(data.Source)
		state.Branch = types.StringValue(data.Branch)
		state.Folder = types.StringValue(data.Folder)
		state.TemplateID = types.StringValue(data.TemplateId)
		state.IaCType = types.StringValue(data.IaCType)
		state.IaCVersion = types.StringValue(data.IaCVersion)
		state.ExecutionMode = types.StringValue(data.ExecutionMode)
		state.Deleted = types.BoolValue(data.Deleted)
		state.AllowRemoteApply = types.BoolValue(data.AllowRemoteApply)
		if data.Vcs != nil {
			state.VCSID = types.StringValue(data.Vcs.ID)
		}
	}

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}
