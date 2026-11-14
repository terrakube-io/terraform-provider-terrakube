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
	client   *http.Client
	endpoint string
	token    string
}

func NewOrganizationVariableDataSource() datasource.DataSource {
	return &OrganizationVariableDataSource{}
}

func (d *OrganizationVariableDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Organization Variable Data Source Configure Type",
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
	tflog.Debug(ctx, "Configured Organization Variable data source")
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
	// The whole filter is escaped, including the ';' that joins the conditions.
	filter := "key==" + rsqlQuote(key)
	if category != "" {
		filter += ";category==" + rsqlQuote(category)
	}
	reqURL := fmt.Sprintf("%s/api/v1/organization/%s/globalvar?filter[globalvar]=%s",
		d.endpoint, url.PathEscape(state.OrganizationId.ValueString()), url.QueryEscape(filter))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating organization variable data source request", fmt.Sprintf("Error creating request: %s", err))
		return
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")

	response, err := d.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Error executing organization variable request", fmt.Sprintf("Error executing request: %s", err))
		return
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading organization variable response body", fmt.Sprintf("Error reading response body: %s", err))
		return
	}
	if reportAPIErrorStatus(&resp.Diagnostics, "reading organization variable", response, body) {
		return
	}

	variables, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(new(client.OrganizationVariableEntity)))
	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal organization variable payload", fmt.Sprintf("Unable to unmarshal payload: %s", err))
		return
	}

	var found *client.OrganizationVariableEntity
	for _, item := range variables {
		variable, ok := item.(*client.OrganizationVariableEntity)
		if !ok || variable == nil {
			resp.Diagnostics.AddError("Unable to unmarshal organization variable payload", "Unexpected type in variable payload.")
			return
		}
		if variable.Key != key || (category != "" && variable.Category != category) {
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
