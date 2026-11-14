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
	_ datasource.DataSource              = &CollectionItemDataSource{}
	_ datasource.DataSourceWithConfigure = &CollectionItemDataSource{}
)

type CollectionItemDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	CollectionId   types.String `tfsdk:"collection_id"`
	Key            types.String `tfsdk:"key"`
	Category       types.String `tfsdk:"category"`
	Value          types.String `tfsdk:"value"`
	Description    types.String `tfsdk:"description"`
	Sensitive      types.Bool   `tfsdk:"sensitive"`
	Hcl            types.Bool   `tfsdk:"hcl"`
}

type CollectionItemDataSource struct {
	client   *http.Client
	endpoint string
	token    string
}

func NewCollectionItemDataSource() datasource.DataSource {
	return &CollectionItemDataSource{}
}

func (d *CollectionItemDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Collection Item Data Source Configure Type",
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
	tflog.Debug(ctx, "Configured Collection Item data source")
}

func (d *CollectionItemDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_collection_item"
}

func (d *CollectionItemDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a collection item by key within a Terrakube collection. The same key can exist in more than one category, so set `category` when it does. `value` is null for sensitive items, and the attribute is always marked sensitive.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Collection item ID",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization ID",
			},
			"collection_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube collection ID",
			},
			"key": schema.StringAttribute{
				Required:    true,
				Description: "Item key",
			},
			"category": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Item category, ENV or TERRAFORM. Required when the key exists in both.",
			},
			"value": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "Item value. Null when the item is sensitive.",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Item description",
			},
			"sensitive": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the item is sensitive",
			},
			"hcl": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the value is HCL",
			},
		},
	}
}

func (d *CollectionItemDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state CollectionItemDataSourceModel
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
	reqURL := fmt.Sprintf("%s/api/v1/organization/%s/collection/%s/item?filter[item]=%s",
		d.endpoint, url.PathEscape(state.OrganizationId.ValueString()), url.PathEscape(state.CollectionId.ValueString()), url.QueryEscape(filter))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating collection item data source request", fmt.Sprintf("Error creating request: %s", err))
		return
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")

	response, err := d.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Error executing collection item request", fmt.Sprintf("Error executing request: %s", err))
		return
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading collection item response body", fmt.Sprintf("Error reading response body: %s", err))
		return
	}
	if reportAPIErrorStatus(&resp.Diagnostics, "reading collection item", response, body) {
		return
	}

	entities, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(new(client.CollectionItemEntity)))
	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal collection item payload", fmt.Sprintf("Unable to unmarshal payload: %s", err))
		return
	}

	var found *client.CollectionItemEntity
	for _, item := range entities {
		entity, ok := item.(*client.CollectionItemEntity)
		if !ok || entity == nil {
			resp.Diagnostics.AddError("Unable to unmarshal collection item payload", "Unexpected type in collection item payload.")
			return
		}
		if entity.Key != key || (category != "" && entity.Category != category) {
			continue
		}
		if found != nil {
			resp.Diagnostics.AddError("Multiple collection items found",
				fmt.Sprintf("More than one item with key %q was found in collection %s of organization %s. Set category to choose one.", key, state.CollectionId.ValueString(), state.OrganizationId.ValueString()))
			return
		}
		found = entity
	}
	if found == nil {
		resp.Diagnostics.AddError("Collection item not found",
			fmt.Sprintf("No item with key %q was found in collection %s of organization %s.", key, state.CollectionId.ValueString(), state.OrganizationId.ValueString()))
		return
	}

	sensitive := found.Sensitive
	state.ID = types.StringValue(found.ID)
	state.Category = types.StringValue(found.Category)
	state.Description = types.StringPointerValue(found.Description)
	state.Sensitive = types.BoolValue(sensitive)
	state.Hcl = types.BoolValue(found.Hcl)
	state.Value = types.StringNull()
	if !sensitive {
		state.Value = types.StringValue(found.Value)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
