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
	_ datasource.DataSource              = &SelfHostedAgentDataSource{}
	_ datasource.DataSourceWithConfigure = &SelfHostedAgentDataSource{}
)

type SelfHostedAgentDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	Url            types.String `tfsdk:"url"`
}

type SelfHostedAgentDataSource struct {
	client   *http.Client
	endpoint string
	token    string
}

func NewSelfHostedAgentDataSource() datasource.DataSource {
	return &SelfHostedAgentDataSource{}
}

func (d *SelfHostedAgentDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Self Hosted Agent Data Source Configure Type",
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
	tflog.Debug(ctx, "Configured Self Hosted Agent data source")
}

func (d *SelfHostedAgentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_self_hosted_agent"
}

func (d *SelfHostedAgentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a self-hosted agent by name within a Terrakube organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Self-hosted agent ID",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization ID",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Self-hosted agent name",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Description of the self-hosted agent",
			},
			"url": schema.StringAttribute{
				Computed:    true,
				Description: "URL of the self-hosted agent",
			},
		},
	}
}

func (d *SelfHostedAgentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state SelfHostedAgentDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	reqURL := fmt.Sprintf("%s/api/v1/organization/%s/agent?filter[agent]=name==%s",
		d.endpoint, url.PathEscape(state.OrganizationId.ValueString()), url.QueryEscape(rsqlQuote(state.Name.ValueString())))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating self-hosted agent data source request", fmt.Sprintf("Error creating request: %s", err))
		return
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")

	response, err := d.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Error executing self-hosted agent request", fmt.Sprintf("Error executing request: %s", err))
		return
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading self-hosted agent response body", fmt.Sprintf("Error reading response body: %s", err))
		return
	}
	if reportAPIErrorStatus(&resp.Diagnostics, "reading self-hosted agent", response, body) {
		return
	}

	agents, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(new(client.AgentEntity)))
	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal self-hosted agent payload", fmt.Sprintf("Unable to unmarshal payload: %s", err))
		return
	}

	var found *client.AgentEntity
	for _, item := range agents {
		agent, ok := item.(*client.AgentEntity)
		if !ok || agent == nil {
			resp.Diagnostics.AddError("Unable to unmarshal self-hosted agent payload", "Unexpected type in agent payload.")
			return
		}
		if agent.Name != state.Name.ValueString() {
			continue
		}
		if found != nil {
			resp.Diagnostics.AddError("Multiple self-hosted agents found",
				fmt.Sprintf("More than one agent named %q was found in organization %s.", state.Name.ValueString(), state.OrganizationId.ValueString()))
			return
		}
		found = agent
	}
	if found == nil {
		resp.Diagnostics.AddError("Self-hosted agent not found",
			fmt.Sprintf("No agent named %q was found in organization %s.", state.Name.ValueString(), state.OrganizationId.ValueString()))
		return
	}

	state.ID = types.StringValue(found.ID)
	state.Description = types.StringValue(found.Description)
	state.Url = types.StringValue(found.Url)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
