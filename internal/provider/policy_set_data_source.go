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
	client *terrakube.Client
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

func (d *PolicySetDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Policy Set Data Source")
}

func (d *PolicySetDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state PolicySetDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// 1. Resolve Organization ID
	orgs, err := d.client.Organizations.List(ctx, &terrakube.ListOptions{Filter: "name==" + state.Organization.ValueString()})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading organization", err)
		return
	}
	if len(orgs) == 0 {
		resp.Diagnostics.AddError("Organization Not Found", fmt.Sprintf("Organization %s was not found.", state.Organization.ValueString()))
		return
	}

	orgData := orgs[0]
	state.OrganizationId = types.StringValue(orgData.ID)

	// 2. Query Policy Set by name
	policySets, err := d.client.PolicySets.List(ctx, orgData.ID, &terrakube.ListOptions{Filter: "name==" + state.Name.ValueString()})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading policy set", err)
		return
	}
	if len(policySets) == 0 {
		resp.Diagnostics.AddError("Policy Set Not Found", fmt.Sprintf("Policy Set %s was not found in organization %s.", state.Name.ValueString(), state.Organization.ValueString()))
		return
	}

	data := policySets[0]
	state.ID = types.StringValue(data.ID)
	state.Name = types.StringValue(stringOrEmpty(data.Name))
	state.Description = types.StringPointerValue(data.Description)
	state.EnforcementLevel = enforcementLevelState(types.StringNull(), stringOrEmpty(data.EnforcementLevel))
	if data.ShadowEnforcementLevel != nil {
		state.ShadowEnforcementLevel = enforcementLevelState(types.StringNull(), *data.ShadowEnforcementLevel)
	} else {
		state.ShadowEnforcementLevel = types.StringNull()
	}
	state.OverrideTeam = types.StringPointerValue(data.OverrideTeam)
	state.Global = types.BoolValue(data.Global)
	state.Repository = types.StringPointerValue(data.Repository)
	state.Branch = types.StringValue(stringOrEmpty(data.Branch))
	state.Folder = types.StringValue(stringOrEmpty(data.Folder))
	if data.VCS != nil {
		state.VcsId = types.StringValue(data.VCS.ID)
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
