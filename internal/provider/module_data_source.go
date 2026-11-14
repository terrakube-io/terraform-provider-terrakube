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
	client   *http.Client
	endpoint string
	token    string
}

func NewModuleDataSource() datasource.DataSource {
	return &ModuleDataSource{}
}

func (d *ModuleDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Module Data Source Configure Type",
			fmt.Sprintf("Expected *TerrakubeConnectionData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
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
	tflog.Debug(ctx, "Configured Module data source")
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
	// The whole filter is escaped, including the ';' that joins the two conditions.
	filter := fmt.Sprintf("name==%s;provider==%s", rsqlQuote(name), rsqlQuote(providerName))
	reqURL := fmt.Sprintf("%s/api/v1/organization/%s/module?filter[module]=%s",
		d.endpoint, url.PathEscape(state.OrganizationId.ValueString()), url.QueryEscape(filter))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating module data source request", fmt.Sprintf("Error creating request: %s", err))
		return
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")

	response, err := d.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Error executing module request", fmt.Sprintf("Error executing request: %s", err))
		return
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading module response body", fmt.Sprintf("Error reading response body: %s", err))
		return
	}
	if reportAPIErrorStatus(&resp.Diagnostics, "reading module", response, body) {
		return
	}

	modules, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(new(client.ModuleEntity)))
	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal module payload", fmt.Sprintf("Unable to unmarshal payload: %s", err))
		return
	}

	var found *client.ModuleEntity
	for _, item := range modules {
		module, ok := item.(*client.ModuleEntity)
		if !ok || module == nil {
			resp.Diagnostics.AddError("Unable to unmarshal module payload", "Unexpected type in module payload.")
			return
		}
		if module.Name != name || module.Provider != providerName {
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
	if found.Ssh != nil {
		state.SshId = types.StringValue(found.Ssh.ID)
	}
	state.TagPrefix = types.StringPointerValue(found.TagPrefix)
	state.Folder = types.StringPointerValue(found.Folder)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
