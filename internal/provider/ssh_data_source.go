package provider

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/google/jsonapi"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"io"
	"net/http"
	"reflect"
	"strings"
	"terraform-provider-terrakube/internal/client"
)

var (
	_ datasource.DataSource              = &SshDataSource{}
	_ datasource.DataSourceWithConfigure = &SshDataSource{}
)

type SshDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
}

type SshDataSource struct {
	client   *http.Client
	endpoint string
	token    string
}

func NewSshDataSource() datasource.DataSource {
	return &SshDataSource{}
}

func (d *SshDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, res *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		res.Diagnostics.AddError(
			"Unexpected Ssh Data Source Configure Type",
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

	tflog.Info(ctx, "Creating Ssh datasource")
}

func (d *SshDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh"
}

func (d *SshDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Ssh Id",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization id",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Ssh Name",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Ssh description information",
			},
		},
	}
}

func (d *SshDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state SshDataSourceModel

	req.Config.Get(ctx, &state)

	requestSsh, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/ssh?filter[ssh]=name==%s", d.endpoint, state.OrganizationId.ValueString(), state.Name.ValueString()), nil)
	requestSsh.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	requestSsh.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		tflog.Error(ctx, "Error creating ssh datasource request")
	}

	responseSsh, err := d.client.Do(requestSsh)
	if err != nil {
		resp.Diagnostics.AddError("Error executing ssh request", fmt.Sprintf("Error executing ssh request: %s", err))
		return
	}

	body, err := io.ReadAll(responseSsh.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading ssh response body", fmt.Sprintf("Error reading ssh response body: %s", err))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(body)})

	var sshList []interface{}

	sshList, err = jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(new(client.SshEntity)))

	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal payload", fmt.Sprintf("Unable to unmarshal payload: %s", err))
		return
	}

	for _, ssh := range sshList {
		data, _ := ssh.(*client.SshEntity)
		state.ID = types.StringValue(data.ID)
		state.Description = types.StringPointerValue(data.Description)
	}

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}
