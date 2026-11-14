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
	_ datasource.DataSource              = &WorkspaceAccessDataSource{}
	_ datasource.DataSourceWithConfigure = &WorkspaceAccessDataSource{}
)

type WorkspaceAccessDataSourceModel struct {
	ID              types.String `tfsdk:"id"`
	OrganizationId  types.String `tfsdk:"organization_id"`
	WorkspaceId     types.String `tfsdk:"workspace_id"`
	Name            types.String `tfsdk:"name"`
	ManageState     types.Bool   `tfsdk:"manage_state"`
	ManageJob       types.Bool   `tfsdk:"manage_job"`
	ManageWorkspace types.Bool   `tfsdk:"manage_workspace"`
	PlanJob         types.Bool   `tfsdk:"plan_job"`
	ApproveJob      types.Bool   `tfsdk:"approve_job"`
	DestroyJob      types.Bool   `tfsdk:"destroy_job"`
	Role            types.String `tfsdk:"role"`
}

type WorkspaceAccessDataSource struct {
	client   *http.Client
	endpoint string
	token    string
}

func NewWorkspaceAccessDataSource() datasource.DataSource {
	return &WorkspaceAccessDataSource{}
}

func (d *WorkspaceAccessDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Workspace access Data Source Configure Type",
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
	tflog.Debug(ctx, "Configured Workspace access data source")
}

func (d *WorkspaceAccessDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_access"
}

func (d *WorkspaceAccessDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up the access a team has to a workspace by team name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Access ID",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization ID",
			},
			"workspace_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube workspace ID",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Team name",
			},
			"manage_state": schema.BoolAttribute{
				Computed:    true,
				Description: "Allowed to manage Terraform/OpenTofu state",
			},
			"manage_job": schema.BoolAttribute{
				Computed:    true,
				Description: "Allowed to manage and trigger jobs",
			},
			"manage_workspace": schema.BoolAttribute{
				Computed:    true,
				Description: "Allowed to manage workspaces",
			},
			"plan_job": schema.BoolAttribute{
				Computed:    true,
				Description: "Allowed to queue plans. Only used when role is unset or custom.",
			},
			"approve_job": schema.BoolAttribute{
				Computed:    true,
				Description: "Allowed to approve and apply runs. Only used when role is unset or custom.",
			},
			"destroy_job": schema.BoolAttribute{
				Computed:    true,
				Description: "Allowed to run destroy (Terrakube 2.34.0+). Null when the API does not return it.",
			},
			"role": schema.StringAttribute{
				Computed:    true,
				Description: "Predefined role: admin, write, plan, read or custom. Null when unset.",
			},
		},
	}
}

func (d *WorkspaceAccessDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state WorkspaceAccessDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	reqURL := fmt.Sprintf("%s/api/v1/organization/%s/workspace/%s/access?filter[access]=name==%s",
		d.endpoint, url.PathEscape(state.OrganizationId.ValueString()), url.PathEscape(state.WorkspaceId.ValueString()), url.QueryEscape(rsqlQuote(name)))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace access data source request", fmt.Sprintf("Error creating request: %s", err))
		return
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")

	response, err := d.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace access request", fmt.Sprintf("Error executing request: %s", err))
		return
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading workspace access response body", fmt.Sprintf("Error reading response body: %s", err))
		return
	}
	if reportAPIErrorStatus(&resp.Diagnostics, "reading workspace access", response, body) {
		return
	}

	entries, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(new(client.WorkspaceAccessEntity)))
	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal workspace access payload", fmt.Sprintf("Unable to unmarshal payload: %s", err))
		return
	}

	var found *client.WorkspaceAccessEntity
	for _, item := range entries {
		access, ok := item.(*client.WorkspaceAccessEntity)
		if !ok || access == nil {
			resp.Diagnostics.AddError("Unable to unmarshal workspace access payload", "Unexpected type in workspace access payload.")
			return
		}
		if access.Name != name {
			continue
		}
		if found != nil {
			resp.Diagnostics.AddError("Multiple workspace access entries found",
				fmt.Sprintf("More than one entry for team %q was found in workspace %s.", name, state.WorkspaceId.ValueString()))
			return
		}
		found = access
	}
	if found == nil {
		resp.Diagnostics.AddError("Workspace access not found",
			fmt.Sprintf("No access for team %q was found in workspace %s.", name, state.WorkspaceId.ValueString()))
		return
	}

	state.ID = types.StringValue(found.ID)
	state.ManageState = types.BoolValue(found.ManageState)
	state.ManageJob = types.BoolValue(found.ManageJob)
	state.ManageWorkspace = types.BoolValue(found.ManageWorkspace)
	state.PlanJob = types.BoolValue(found.PlanJob)
	state.ApproveJob = types.BoolValue(found.ApproveJob)
	state.DestroyJob = types.BoolPointerValue(found.DestroyJob)
	state.Role = roleToState(found.Role)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
