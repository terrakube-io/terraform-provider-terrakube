package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	terrakube "github.com/terrakube-io/terrakube-go"
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
	client *terrakube.Client
}

func NewSshDataSource() datasource.DataSource {
	return &SshDataSource{}
}

func (d *SshDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Ssh Data Source")
}

func (d *SshDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh"
}

func (d *SshDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an SSH key by name. Use `id` as a workspace's `ssh_id` to clone its repository over SSH, or as `module_ssh_key` to download private modules from Git. The private key itself is never returned.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "ID of the SSH key.",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "ID of the organization the SSH key belongs to.",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Name of the SSH key.",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Description of the SSH key.",
			},
		},
	}
}

func (d *SshDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state SshDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sshKeys, err := d.client.SSH.List(ctx, state.OrganizationId.ValueString(), &terrakube.ListOptions{Filter: "name==" + rsqlQuote(state.Name.ValueString())})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading ssh key", err)
		return
	}

	data := findSingleByNameIn(&resp.Diagnostics, "ssh key", state.Name.ValueString(), " in organization "+state.OrganizationId.ValueString(), sshKeys,
		func(e *terrakube.SSH) string { return e.Name })
	if data == nil {
		return
	}
	// The private key is not part of the data source.
	state.ID = types.StringValue(data.ID)
	state.Description = types.StringPointerValue(data.Description)

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
