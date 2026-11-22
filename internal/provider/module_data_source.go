package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	terrakube "github.com/terrakube-io/terrakube-go"
)

var (
	_ datasource.DataSource              = &ModuleDataSource{}
	_ datasource.DataSourceWithConfigure = &ModuleDataSource{}
)

type ModuleDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Name           types.String `tfsdk:"name"`
	ProviderName   types.String `tfsdk:"provider_name"`
	Description    types.String `tfsdk:"description"`
	Source         types.String `tfsdk:"source"`
	VcsId          types.String `tfsdk:"vcs_id"`
	SshId          types.String `tfsdk:"ssh_id"`
	TagPrefix      types.String `tfsdk:"tag_prefix"`
	Folder         types.String `tfsdk:"folder"`
}

type ModuleDataSource struct {
	client *terrakube.Client
}

func NewModuleDataSource() datasource.DataSource {
	return &ModuleDataSource{}
}

func (d *ModuleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Module Data Source")
}

func (d *ModuleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_module"
}

func (d *ModuleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a private registry module by name and provider within a Terrakube organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Module ID",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization ID",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Module name",
			},
			"provider_name": schema.StringAttribute{
				Required:    true,
				Description: "Module provider name. Example: azurerm, google, aws, etc",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Module description",
			},
			"source": schema.StringAttribute{
				Computed:    true,
				Description: "Source repository of the module",
			},
			"vcs_id": schema.StringAttribute{
				Computed:    true,
				Description: "VCS connection ID, null when the module does not use one",
			},
			"ssh_id": schema.StringAttribute{
				Computed:    true,
				Description: "SSH connection ID, null when the module does not use one",
			},
			"tag_prefix": schema.StringAttribute{
				Computed:    true,
				Description: "Tag prefix for mono-repository modules",
			},
			"folder": schema.StringAttribute{
				Computed:    true,
				Description: "Folder of the module inside the repository",
			},
		},
	}
}

func (d *ModuleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state ModuleDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	providerName := state.ProviderName.ValueString()
	filter := fmt.Sprintf("name==%s;provider==%s", rsqlQuote(name), rsqlQuote(providerName))
	modules, err := d.client.Modules.List(ctx, state.OrganizationId.ValueString(), &terrakube.ListOptions{Filter: filter})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading module", err)
		return
	}

	var found *terrakube.Module
	for _, module := range modules {
		if module == nil || module.Name != name || module.Provider != providerName {
			continue
		}
		if found != nil {
			resp.Diagnostics.AddError("Multiple modules found",
				fmt.Sprintf("More than one module named %q for provider %q was found in organization %s.", name, providerName, state.OrganizationId.ValueString()))
			return
		}
		found = module
	}
	if found == nil {
		resp.Diagnostics.AddError("Module not found",
			fmt.Sprintf("No module named %q for provider %q was found in organization %s.", name, providerName, state.OrganizationId.ValueString()))
		return
	}

	state.ID = types.StringValue(found.ID)
	state.Description = types.StringValue(found.Description)
	state.Source = types.StringValue(found.Source)
	state.VcsId = types.StringNull()
	if found.Vcs != nil {
		state.VcsId = types.StringValue(found.Vcs.ID)
	}
	state.SshId = types.StringNull()
	if found.SSH != nil {
		state.SshId = types.StringValue(found.SSH.ID)
	}
	state.TagPrefix = types.StringPointerValue(found.TagPrefix)
	state.Folder = types.StringPointerValue(found.Folder)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
