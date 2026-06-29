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
	_ datasource.DataSource              = &FederatedCredentialDataSource{}
	_ datasource.DataSourceWithConfigure = &FederatedCredentialDataSource{}
)

type FederatedCredentialDataSourceModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	IssuerUrl types.String `tfsdk:"issuer_url"`
	Audience  types.String `tfsdk:"audience"`
}

type FederatedCredentialDataSource struct {
	client   *http.Client
	endpoint string
	token    string
}

func NewFederatedCredentialDataSource() datasource.DataSource {
	return &FederatedCredentialDataSource{}
}

func (d *FederatedCredentialDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, res *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		res.Diagnostics.AddError(
			"Unexpected Federated Credential Data Source Configure Type",
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

	tflog.Info(ctx, "Creating Federated Credential datasource")
}

func (d *FederatedCredentialDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_federated_credential"
}

func (d *FederatedCredentialDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Federated credential ID",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Federated credential name",
			},
			"issuer_url": schema.StringAttribute{
				Computed:    true,
				Description: "Issuer URL of the external identity provider",
			},
			"audience": schema.StringAttribute{
				Computed:    true,
				Description: "Audience expected in tokens issued by the identity provider",
			},
		},
	}
}

func (d *FederatedCredentialDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state FederatedCredentialDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	reqURL := fmt.Sprintf("%s/api/v1/federated?filter[federated]=name==%s", d.endpoint, state.Name.ValueString())
	federatedRequest, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating federated credential datasource request", fmt.Sprintf("Error creating federated credential datasource request: %s", err))
		return
	}
	federatedRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	federatedRequest.Header.Add("Content-Type", "application/vnd.api+json")

	federatedResponse, err := d.client.Do(federatedRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing federated credential request", fmt.Sprintf("Error executing federated credential request: %s", err))
		return
	}

	body, err := io.ReadAll(federatedResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading federated credential response body", fmt.Sprintf("Error reading federated credential response body: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(body)})

	federatedList, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(new(client.FederatedEntity)))
	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal payload", fmt.Sprintf("Unable to unmarshal payload: %s", err))
		return
	}

	if len(federatedList) == 0 {
		resp.Diagnostics.AddError("Federated credential not found", fmt.Sprintf("No federated credential found with name: %s", state.Name.ValueString()))
		return
	}

	for _, federated := range federatedList {
		data, _ := federated.(*client.FederatedEntity)
		state.ID = types.StringValue(data.ID)
		state.Name = types.StringValue(data.Name)
		state.IssuerUrl = types.StringValue(data.IssuerUrl)
		state.Audience = types.StringValue(data.Audience)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
