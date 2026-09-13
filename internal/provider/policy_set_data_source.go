package provider

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
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
	_ datasource.DataSource              = &PolicySetDataSource{}
	_ datasource.DataSourceWithConfigure = &PolicySetDataSource{}
)

type PolicySetDataSourceModel struct {
	ID                          types.String `tfsdk:"id"`
	Organization                types.String `tfsdk:"organization"`
	OrganizationId              types.String `tfsdk:"organization_id"`
	Name                        types.String `tfsdk:"name"`
	Description                 types.String `tfsdk:"description"`
	EnforcementLevel            types.String `tfsdk:"enforcement_level"`
	ShadowEnforcementLevel      types.String `tfsdk:"shadow_enforcement_level"`
	OverrideTeam                types.String `tfsdk:"override_team"`
	Global                      types.Bool   `tfsdk:"global"`
	Repository                  types.String `tfsdk:"repository"`
	Branch                      types.String `tfsdk:"branch"`
	Folder                      types.String `tfsdk:"folder"`
	VcsId                       types.String `tfsdk:"vcs_id"`
	NotificationConfigurationId types.String `tfsdk:"notification_configuration_id"`
	OpaVersion                  types.String `tfsdk:"opa_version"`
}

type PolicySetDataSource struct {
	client   *http.Client
	endpoint string
	token    string
}

func NewPolicySetDataSource() datasource.DataSource {
	return &PolicySetDataSource{}
}

func (d *PolicySetDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_policy_set"
}

func (d *PolicySetDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Query a Terrakube OPA Policy Set by organization name and policy set name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Policy Set ID (UUID)",
			},
			"organization": schema.StringAttribute{
				Required:    true,
				Description: "Organization Name",
			},
			"organization_id": schema.StringAttribute{
				Computed:    true,
				Description: "Organization ID (UUID)",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Policy Set Name",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Description of the policy set",
			},
			"enforcement_level": schema.StringAttribute{
				Computed:    true,
				Description: "Enforcement level: hard_mandatory, soft_mandatory, or advisory",
			},
			"shadow_enforcement_level": schema.StringAttribute{
				Computed:    true,
				Description: "Shadow enforcement level if active",
			},
			"override_team": schema.StringAttribute{
				Computed:    true,
				Description: "Team authorized to override soft_mandatory violations",
			},
			"global": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether this policy set applies globally to all workspaces",
			},
			"repository": schema.StringAttribute{
				Computed:    true,
				Description: "Git repository URL",
			},
			"branch": schema.StringAttribute{
				Computed:    true,
				Description: "Git branch or SemVer tag",
			},
			"folder": schema.StringAttribute{
				Computed:    true,
				Description: "Subfolder path for Rego policies",
			},
			"vcs_id": schema.StringAttribute{
				Computed:    true,
				Description: "VCS connection ID",
			},
			"notification_configuration_id": schema.StringAttribute{
				Computed:    true,
				Description: "Notification configuration ID",
			},
			"opa_version": schema.StringAttribute{
				Computed:    true,
				Description: "OPA binary version",
			},
		},
	}
}

func (d *PolicySetDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, res *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		res.Diagnostics.AddError(
			"Unexpected Policy Set Data Source Configure Type",
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

	tflog.Info(ctx, "Configured Policy Set datasource", map[string]any{"success": true})
}

func (d *PolicySetDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state PolicySetDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// 1. Resolve Organization ID
	orgReq, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization?filter[organization]=name==%s", d.endpoint, state.Organization.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating organization lookup request", fmt.Sprintf("Error: %s", err))
		return
	}
	orgReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	orgReq.Header.Add("Content-Type", "application/vnd.api+json")

	orgResp, err := d.client.Do(orgReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing organization lookup request", fmt.Sprintf("Error: %s", err))
		return
	}
	defer orgResp.Body.Close()

	orgBody, err := io.ReadAll(orgResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading organization response", fmt.Sprintf("Error: %s", err))
		return
	}

	orgs, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(orgBody)), reflect.TypeOf(new(client.OrganizationEntity)))
	if err != nil || len(orgs) == 0 {
		resp.Diagnostics.AddError("Organization Not Found", fmt.Sprintf("Organization %s was not found.", state.Organization.ValueString()))
		return
	}

	orgData, _ := orgs[0].(*client.OrganizationEntity)
	state.OrganizationId = types.StringValue(orgData.ID)

	// 2. Query Policy Set by name
	psReq, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/policySet?filter[policy_set]=name==%s", d.endpoint, orgData.ID, state.Name.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating policy set query request", fmt.Sprintf("Error: %s", err))
		return
	}
	psReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	psReq.Header.Add("Content-Type", "application/vnd.api+json")

	psResp, err := d.client.Do(psReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing policy set query request", fmt.Sprintf("Error: %s", err))
		return
	}
	defer psResp.Body.Close()

	psBody, err := io.ReadAll(psResp.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading policy set response", fmt.Sprintf("Error: %s", err))
		return
	}

	policySets, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(psBody)), reflect.TypeOf(new(client.PolicySetEntity)))
	if err != nil || len(policySets) == 0 {
		resp.Diagnostics.AddError("Policy Set Not Found", fmt.Sprintf("Policy Set %s was not found in organization %s.", state.Name.ValueString(), state.Organization.ValueString()))
		return
	}

	data, _ := policySets[0].(*client.PolicySetEntity)
	state.ID = types.StringValue(data.ID)
	state.Name = types.StringValue(data.Name)
	state.Description = types.StringPointerValue(data.Description)
	state.EnforcementLevel = types.StringValue(strings.ToLower(data.EnforcementLevel))
	if data.ShadowEnforcementLevel != nil {
		state.ShadowEnforcementLevel = types.StringValue(strings.ToLower(*data.ShadowEnforcementLevel))
	} else {
		state.ShadowEnforcementLevel = types.StringNull()
	}
	state.OverrideTeam = types.StringPointerValue(data.OverrideTeam)
	state.Global = types.BoolValue(data.Global)
	state.Repository = types.StringPointerValue(data.Repository)
	state.Branch = types.StringValue(data.Branch)
	state.Folder = types.StringValue(data.Folder)
	if data.Vcs != nil {
		state.VcsId = types.StringValue(data.Vcs.ID)
	} else {
		state.VcsId = types.StringNull()
	}
	if data.NotificationConfiguration != nil {
		state.NotificationConfigurationId = types.StringValue(data.NotificationConfiguration.ID)
	} else {
		state.NotificationConfigurationId = types.StringNull()
	}
	state.OpaVersion = types.StringPointerValue(data.OpaVersion)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
