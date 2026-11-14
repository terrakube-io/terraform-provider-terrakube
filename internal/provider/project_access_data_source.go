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
	_ datasource.DataSource              = &ProjectAccessDataSource{}
	_ datasource.DataSourceWithConfigure = &ProjectAccessDataSource{}
)

type ProjectAccessDataSourceModel struct {
	ID              types.String `tfsdk:"id"`
	OrganizationId  types.String `tfsdk:"organization_id"`
	ProjectId       types.String `tfsdk:"project_id"`
	Name            types.String `tfsdk:"name"`
	ManageState     types.Bool   `tfsdk:"manage_state"`
	ManageJob       types.Bool   `tfsdk:"manage_job"`
	ManageWorkspace types.Bool   `tfsdk:"manage_workspace"`
	PlanJob         types.Bool   `tfsdk:"plan_job"`
	ApproveJob      types.Bool   `tfsdk:"approve_job"`
	DestroyJob      types.Bool   `tfsdk:"destroy_job"`
	Role            types.String `tfsdk:"role"`
}

type ProjectAccessDataSource struct {
	client   *http.Client
	endpoint string
	token    string
}

func NewProjectAccessDataSource() datasource.DataSource {
	return &ProjectAccessDataSource{}
}

func (d *ProjectAccessDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Project access Data Source Configure Type",
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
	tflog.Debug(ctx, "Configured Project access data source")
}

func (d *ProjectAccessDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_access"
}

func (d *ProjectAccessDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up the access a team has to a project by team name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Access ID",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization ID",
			},
			"project_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube project ID",
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

func (d *ProjectAccessDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state ProjectAccessDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	reqURL := fmt.Sprintf("%s/api/v1/organization/%s/project/%s/projectAccess?filter[project_access]=name==%s",
		d.endpoint, url.PathEscape(state.OrganizationId.ValueString()), url.PathEscape(state.ProjectId.ValueString()), url.QueryEscape(rsqlQuote(name)))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating project access data source request", fmt.Sprintf("Error creating request: %s", err))
		return
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")

	response, err := d.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Error executing project access request", fmt.Sprintf("Error executing request: %s", err))
		return
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading project access response body", fmt.Sprintf("Error reading response body: %s", err))
		return
	}
	if reportAPIErrorStatus(&resp.Diagnostics, "reading project access", response, body) {
		return
	}

	entries, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(new(client.ProjectAccessEntity)))
	if err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal project access payload", fmt.Sprintf("Unable to unmarshal payload: %s", err))
		return
	}

	var found *client.ProjectAccessEntity
	for _, item := range entries {
		access, ok := item.(*client.ProjectAccessEntity)
		if !ok || access == nil {
			resp.Diagnostics.AddError("Unable to unmarshal project access payload", "Unexpected type in project access payload.")
			return
		}
		if access.Name != name {
			continue
		}
		if found != nil {
			resp.Diagnostics.AddError("Multiple project access entries found",
				fmt.Sprintf("More than one entry for team %q was found in project %s.", name, state.ProjectId.ValueString()))
			return
		}
		found = access
	}
	if found == nil {
		resp.Diagnostics.AddError("Project access not found",
			fmt.Sprintf("No access for team %q was found in project %s.", name, state.ProjectId.ValueString()))
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
