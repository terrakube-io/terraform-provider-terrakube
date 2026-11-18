package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ ephemeral.EphemeralResource              = &OutputEphemeralResource{}
	_ ephemeral.EphemeralResourceWithConfigure = &OutputEphemeralResource{}
)

// OutputEphemeralResource reads the outputs of another workspace without
// writing them to plan or state.
type OutputEphemeralResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type OutputEphemeralResourceModel struct {
	Organization       types.String  `tfsdk:"organization"`
	Workspace          types.String  `tfsdk:"workspace"`
	Values             types.Dynamic `tfsdk:"values"`
	NonSensitiveValues types.Dynamic `tfsdk:"nonsensitive_values"`
}

func NewOutputEphemeralResource() ephemeral.EphemeralResource {
	return &OutputEphemeralResource{}
}

func (e *OutputEphemeralResource) Configure(_ context.Context, req ephemeral.ConfigureRequest, resp *ephemeral.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Output Ephemeral Resource Configure Type",
			fmt.Sprintf("Expected *TerrakubeConnectionData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	e.client = newTerrakubeHTTPClient(providerData)
	e.endpoint = providerData.Endpoint
	e.token = providerData.Token
}

func (e *OutputEphemeralResource) Metadata(_ context.Context, req ephemeral.MetadataRequest, resp *ephemeral.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_output"
}

func (e *OutputEphemeralResource) Schema(_ context.Context, _ ephemeral.SchemaRequest, resp *ephemeral.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads the outputs of the latest run of a workspace without storing them in plan or state. " +
			"Use it instead of the `terrakube_output` data source when the outputs include secrets. Requires Terraform 1.10 or later, or OpenTofu 1.11 or later.\n\n" +
			"Its values are ephemeral, so they can only be used where ephemeral values are allowed: provider blocks, write-only arguments " +
			"(such as `value_wo` on `terrakube_workspace_variable`), `locals`, other ephemeral resources, and outputs and variables marked `ephemeral` in a child module. " +
			"Using them in a regular resource argument or a root module output is an error.",
		Attributes: map[string]schema.Attribute{
			"organization": schema.StringAttribute{
				Required:    true,
				Description: "Organization Name",
			},
			"workspace": schema.StringAttribute{
				Required:    true,
				Description: "Workspace Name",
			},
			"values": schema.DynamicAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "Values of all the workspace outputs, including sensitive ones. Null when the workspace has no runs yet.",
			},
			"nonsensitive_values": schema.DynamicAttribute{
				Computed:    true,
				Description: "Values of the workspace outputs that are not marked sensitive. Null when the workspace has no runs yet.",
			},
		},
	}
}

func (e *OutputEphemeralResource) Open(ctx context.Context, req ephemeral.OpenRequest, resp *ephemeral.OpenResponse) {
	var data OutputEphemeralResourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	outputsClient := workspaceOutputsClient{client: e.client, endpoint: e.endpoint, token: e.token}
	outputs, found := outputsClient.read(ctx, data.Organization.ValueString(), data.Workspace.ValueString(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	data.Values = types.DynamicNull()
	data.NonSensitiveValues = types.DynamicNull()
	if found {
		data.Values = outputs.values
		data.NonSensitiveValues = outputs.nonSensitive
	}
	resp.Diagnostics.Append(resp.Result.Set(ctx, &data)...)
}
