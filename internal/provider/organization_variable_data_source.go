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
	_ datasource.DataSource              = &OrganizationVariableDataSource{}
	_ datasource.DataSourceWithConfigure = &OrganizationVariableDataSource{}
)

type OrganizationVariableDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Key            types.String `tfsdk:"key"`
	Category       types.String `tfsdk:"category"`
	Value          types.String `tfsdk:"value"`
	Description    types.String `tfsdk:"description"`
	Sensitive      types.Bool   `tfsdk:"sensitive"`
	Hcl            types.Bool   `tfsdk:"hcl"`
}

type OrganizationVariableDataSource struct {
	client *terrakube.Client
}

func NewOrganizationVariableDataSource() datasource.DataSource {
	return &OrganizationVariableDataSource{}
}

func (d *OrganizationVariableDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Organization Variable Data Source")
}

func (d *OrganizationVariableDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_variable"
}

func (d *OrganizationVariableDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a organization variable by key within a Terrakube organization. The same key can exist in more than one category, so set `category` when it does. `value` is null for sensitive variables, and the attribute is always marked sensitive.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Variable ID",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization ID",
			},
			"key": schema.StringAttribute{
				Required:    true,
				Description: "Variable key",
			},
			"category": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Variable category, ENV or TERRAFORM. Required when the key exists in both.",
			},
			"value": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "Variable value. Null when the variable is sensitive.",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Variable description",
			},
			"sensitive": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the variable is sensitive",
			},
			"hcl": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the value is HCL",
			},
		},
	}
}

func (d *OrganizationVariableDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state OrganizationVariableDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key := state.Key.ValueString()
	category := state.Category.ValueString()
	filter := "key==" + rsqlQuote(key)
	if category != "" {
		filter += ";category==" + rsqlQuote(category)
	}
	variables, err := d.client.OrganizationVariables.List(ctx, state.OrganizationId.ValueString(), &terrakube.ListOptions{Filter: filter})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading organization variable", err)
		return
	}

	var found *terrakube.OrganizationVariable
	for _, variable := range variables {
		if variable == nil || variable.Key != key || (category != "" && variable.Category != category) {
			continue
		}
		if found != nil {
			resp.Diagnostics.AddError("Multiple organization variables found",
				fmt.Sprintf("More than one variable with key %q was found in organization %s. Set category to choose one.", key, state.OrganizationId.ValueString()))
			return
		}
		found = variable
	}
	if found == nil {
		resp.Diagnostics.AddError("Organization variable not found",
			fmt.Sprintf("No variable with key %q was found in organization %s.", key, state.OrganizationId.ValueString()))
		return
	}

	sensitive := found.Sensitive != nil && *found.Sensitive
	state.ID = types.StringValue(found.ID)
	state.Category = types.StringValue(found.Category)
	state.Description = types.StringValue(found.Description)
	state.Sensitive = types.BoolValue(sensitive)
	state.Hcl = types.BoolValue(found.Hcl)
	state.Value = types.StringNull()
	if !sensitive {
		state.Value = types.StringValue(found.Value)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
