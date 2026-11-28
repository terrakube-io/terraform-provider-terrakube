package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	terrakube "github.com/terrakube-io/terrakube-go"
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
	client *terrakube.Client
}

func NewFederatedCredentialDataSource() datasource.DataSource {
	return &FederatedCredentialDataSource{}
}

func (d *FederatedCredentialDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Federated Credential Data Source")
}

func (d *FederatedCredentialDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_federated_credential"
}

func (d *FederatedCredentialDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a federated credential by name. A federated credential lets Terrakube accept tokens from an external identity provider, such as GitHub Actions, GitLab CI or Azure AD. Use `id` to add claim conditions with `terrakube_federated_credential_claim`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "ID of the federated credential.",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Name of the federated credential.",
			},
			"issuer_url": schema.StringAttribute{
				Computed:    true,
				Description: "Issuer URL of the external identity provider.",
			},
			"audience": schema.StringAttribute{
				Computed:    true,
				Description: "Audience that tokens from the identity provider must have.",
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

	credentials, err := d.client.Federated.List(ctx, &terrakube.ListOptions{Filter: "name==" + rsqlQuote(state.Name.ValueString())})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading federated credential", err)
		return
	}

	data := findSingleByNameIn(&resp.Diagnostics, "federated credential", state.Name.ValueString(), "", credentials,
		func(e *terrakube.Federated) string { return e.Name })
	if data == nil {
		return
	}
	state.ID = types.StringValue(data.ID)
	state.Name = types.StringValue(data.Name)
	state.IssuerUrl = types.StringValue(data.IssuerURL)
	state.Audience = types.StringValue(data.Audience)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
