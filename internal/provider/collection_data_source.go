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
	_ datasource.DataSource              = &CollectionDataSource{}
	_ datasource.DataSourceWithConfigure = &CollectionDataSource{}
)

type CollectionDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	Priority       types.Int32  `tfsdk:"priority"`
}

type CollectionDataSource struct {
	client   *http.Client
	endpoint string
	token    string
}

func NewCollectionDataSource() datasource.DataSource {
	return &CollectionDataSource{}
}

func (d *CollectionDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Collection Data Source Configure Type",
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
	tflog.Debug(ctx, "Configured Collection data source")
}

func (d *CollectionDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_collection"
}

func (d *CollectionDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a variable collection by name within a Terrakube organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Collection ID",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization ID",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Collection name",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Collection description, null when it has none",
			},
			"priority": schema.Int32Attribute{
				Computed:    true,
				Description: "Collection priority",
			},
		},
	}
}

func (d *CollectionDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state CollectionDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	reqURL := fmt.Sprintf("%s/api/v1/organization/%s/collection?filter[collection]=name==%s",
		d.endpoint, url.PathEscape(state.OrganizationId.ValueString()), url.QueryEscape(rsqlQuote(name)))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating collection data source request", fmt.Sprintf("Error creating request: %s", err))
		return
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")

	response, err := d.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Error executing collection request", fmt.Sprintf("Error executing request: %s", err))
		return
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading collection response body", fmt.Sprintf("Error reading response body: %s", err))
		return
	}
	if reportAPIErrorStatus(&resp.Diagnostics, "reading collection", response, body) {
		return
	}

	collections, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(new(client.CollectionEntity)))
	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal collection payload", fmt.Sprintf("Unable to unmarshal payload: %s", err))
		return
	}

	var found *client.CollectionEntity
	for _, item := range collections {
		collection, ok := item.(*client.CollectionEntity)
		if !ok || collection == nil {
			resp.Diagnostics.AddError("Unable to unmarshal collection payload", "Unexpected type in collection payload.")
			return
		}
		if collection.Name != name {
			continue
		}
		if found != nil {
			resp.Diagnostics.AddError("Multiple collections found",
				fmt.Sprintf("More than one collection named %q was found in organization %s.", name, state.OrganizationId.ValueString()))
			return
		}
		found = collection
	}
	if found == nil {
		resp.Diagnostics.AddError("Collection not found",
			fmt.Sprintf("No collection named %q was found in organization %s.", name, state.OrganizationId.ValueString()))
		return
	}

	state.ID = types.StringValue(found.ID)
	state.Description = types.StringPointerValue(found.Description)
	state.Priority = types.Int32Value(found.Priority)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
