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
	_ datasource.DataSource              = &WorkspaceTagDataSource{}
	_ datasource.DataSourceWithConfigure = &WorkspaceTagDataSource{}
)

type WorkspaceTagDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	OrganizationId types.String `tfsdk:"organization_id"`
	WorkspaceId    types.String `tfsdk:"workspace_id"`
	TagID          types.String `tfsdk:"tag_id"`
	Value          types.String `tfsdk:"value"`
}

type WorkspaceTagDataSource struct {
	client   *http.Client
	endpoint string
	token    string
}

func NewWorkspaceTagDataSource() datasource.DataSource {
	return &WorkspaceTagDataSource{}
}

func (d *WorkspaceTagDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_tag"
}

func (d *WorkspaceTagDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Query a tag attached to a Terrakube workspace by the tag name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Workspace Tag Id",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "The name of the tag",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization id",
			},
			"workspace_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube workspace id",
			},
			"tag_id": schema.StringAttribute{
				Computed:    true,
				Description: "Organization Tag Id",
			},
			"value": schema.StringAttribute{
				Computed:    true,
				Description: "Value of a key/value tag. Null for a key-only tag or when the Terrakube version does not support tag values.",
			},
		},
	}
}

func (d *WorkspaceTagDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, res *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		res.Diagnostics.AddError(
			"Unexpected Workspace Tag Data Source Configure Type",
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

	tflog.Info(ctx, "Configured Workspace Tag datasource", map[string]any{"success": true})
}

func (d *WorkspaceTagDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state WorkspaceTagDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organizationId := state.OrganizationId.ValueString()
	workspaceId := state.WorkspaceId.ValueString()
	name := state.Name.ValueString()

	// 1. Resolve the organization tag id from the tag name
	tagBody, ok := d.get(fmt.Sprintf("%s/api/v1/organization/%s/tag?filter[tag]=name==%s", d.endpoint, organizationId, url.QueryEscape(rsqlQuote(name))), "organization tag", resp)
	if !ok {
		return
	}

	tags, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(tagBody)), reflect.TypeOf(new(client.OrganizationTagEntity)))
	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal organization tag payload", fmt.Sprintf("Unable to unmarshal payload, error: %s, response body: %s", err, tagBody))
		return
	}

	tagId := ""
	for _, tag := range tags {
		data, _ := tag.(*client.OrganizationTagEntity)
		if data != nil && data.Name == name {
			tagId = data.ID
			break
		}
	}
	if tagId == "" {
		resp.Diagnostics.AddError("Tag Not Found", fmt.Sprintf("Tag %s was not found in organization %s.", name, organizationId))
		return
	}

	// 2. Find the workspace tag that references the organization tag.
	// Filtering is done client side so it does not depend on server side filter support.
	wsTagBody, ok := d.get(fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/workspaceTag", d.endpoint, organizationId, workspaceId), "workspace tag", resp)
	if !ok {
		return
	}

	workspaceTags, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(wsTagBody)), reflect.TypeOf(new(client.WorkspaceTagEntity)))
	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal workspace tag payload", fmt.Sprintf("Unable to unmarshal payload, error: %s, response body: %s", err, wsTagBody))
		return
	}

	for _, workspaceTag := range workspaceTags {
		data, _ := workspaceTag.(*client.WorkspaceTagEntity)
		if data != nil && data.TagID == tagId {
			state.ID = types.StringValue(data.ID)
			state.TagID = types.StringValue(data.TagID)
			state.Value = workspaceTagValue(data.Value)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}

	resp.Diagnostics.AddError("Workspace Tag Not Found", fmt.Sprintf("Tag %s is not attached to workspace %s.", name, workspaceId))
}

func (d *WorkspaceTagDataSource) get(requestUrl string, kind string, resp *datasource.ReadResponse) ([]byte, bool) {
	request, err := http.NewRequest(http.MethodGet, requestUrl, nil)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Error creating %s datasource request", kind), fmt.Sprintf("Error creating %s datasource request: %s", kind, err))
		return nil, false
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")

	response, err := d.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Error executing %s datasource request", kind), fmt.Sprintf("Error executing %s datasource request: %s", kind, err))
		return nil, false
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Error reading %s datasource response", kind), fmt.Sprintf("Error reading %s datasource response: %s", kind, err))
		return nil, false
	}

	if reportAPIErrorStatus(&resp.Diagnostics, "reading "+kind, response, body) {
		return nil, false
	}

	return body, true
}

// rsqlQuote quotes a value for an RSQL filter so names with spaces or reserved characters are accepted.
func rsqlQuote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\`, `"`, `\"`).Replace(value) + `"`
}
