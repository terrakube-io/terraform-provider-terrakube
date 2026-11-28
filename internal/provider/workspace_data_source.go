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
	_ datasource.DataSource              = &WorkspaceDataSource{}
	_ datasource.DataSourceWithConfigure = &WorkspaceDataSource{}
)

type WorkspaceDataSource struct {
	client *terrakube.Client
}

type WorkspaceDataSourceModel struct {
	RestrictApplyToConfiguredBranch types.Bool   `tfsdk:"restrict_apply_to_configured_branch"`
	RequireApproveToApply           types.Bool   `tfsdk:"require_approve_to_apply"`
	ID                              types.String `tfsdk:"id"`
	Name                            types.String `tfsdk:"name"`
	Description                     types.String `tfsdk:"description"`
	Organization                    types.String `tfsdk:"organization"`
	OrganizationID                  types.String `tfsdk:"organization_id"`
	Source                          types.String `tfsdk:"source"`
	Branch                          types.String `tfsdk:"branch"`
	Folder                          types.String `tfsdk:"folder"`
	TemplateID                      types.String `tfsdk:"template_id"`
	IaCType                         types.String `tfsdk:"iactype"`
	IaCVersion                      types.String `tfsdk:"iacversion"`
	ExecutionMode                   types.String `tfsdk:"executionmode"`
	Deleted                         types.Bool   `tfsdk:"deleted"`
	AllowRemoteApply                types.Bool   `tfsdk:"allowremoteapply"`
	VCSID                           types.String `tfsdk:"vcsid"`
	SSHID                           types.String `tfsdk:"sshid"`
	ModuleSshKey                    types.String `tfsdk:"module_ssh_key"`
	PolicyComplianceStatus          types.String `tfsdk:"policy_compliance_status"`
}

func NewWorkspaceDataSource() datasource.DataSource {
	return &WorkspaceDataSource{}
}

func (d *WorkspaceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Workspace Data Source")
}

func (d *WorkspaceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

func (d *WorkspaceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a workspace by name and reads its settings. Unlike most data sources, it takes the organization's name, not its ID, and returns `organization_id` as well.",
		Attributes: map[string]schema.Attribute{
			"restrict_apply_to_configured_branch": schema.BoolAttribute{Computed: true, Description: "Restrict infrastructure-changing runs to the configured workspace branch (Terrakube 2.34.0+)"},
			"require_approve_to_apply":            schema.BoolAttribute{Computed: true, Description: "Require approval permission to start an apply without a preceding approval step (Terrakube 2.34.0+)"},
			"id": schema.StringAttribute{
				Description: "ID of the workspace.",
				Computed:    true,
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Name of the workspace.",
			},
			"organization": schema.StringAttribute{
				Required:    true,
				Description: "Name of the organization the workspace belongs to.",
			},
			"description": schema.StringAttribute{
				Description: "Description of the workspace.",
				Computed:    true,
			},
			"organization_id": schema.StringAttribute{
				Description: "ID of the organization the workspace belongs to.",
				Computed:    true,
			},
			"source": schema.StringAttribute{
				Description: "Repository URL of a VCS workspace. CLI workspaces have `empty`.",
				Computed:    true,
			},
			"branch": schema.StringAttribute{
				Description: "Branch the workspace runs from. CLI workspaces have `remote-content`.",
				Computed:    true,
			},
			"folder": schema.StringAttribute{
				Description: "Folder in the repository where the workspace runs.",
				Computed:    true,
			},
			"template_id": schema.StringAttribute{
				Description: "ID of the workspace's default template.",
				Computed:    true,
			},
			"iactype": schema.StringAttribute{
				Description: "IaC tool of the workspace: `terraform` or `tofu`.",
				Computed:    true,
			},
			"iacversion": schema.StringAttribute{
				Description: "Version of the IaC tool.",
				Computed:    true,
			},
			"executionmode": schema.StringAttribute{
				Description: "Execution mode of the workspace: `remote` or `local`.",
				Computed:    true,
			},
			"deleted": schema.BoolAttribute{
				Description: "Whether the workspace is marked as deleted.",
				Computed:    true,
			},
			"allowremoteapply": schema.BoolAttribute{
				Description: "Whether a VCS workspace allows an apply started from the CLI.",
				Computed:    true,
			},
			"vcsid": schema.StringAttribute{
				Description: "ID of the VCS connection the workspace clones its repository with. Empty for a public repository or a CLI workspace.",
				Computed:    true,
			},
			"sshid": schema.StringAttribute{
				Description: "SSH key ID used to clone the workspace repository directly over SSH",
				Computed:    true,
			},
			"module_ssh_key": schema.StringAttribute{
				Description: "SSH key ID used to download private Terraform/OpenTofu modules referenced via git-based module sources within this workspace",
				Computed:    true,
			},
			"policy_compliance_status": schema.StringAttribute{
				Description: "OPA Policy compliance status: UNKNOWN, COMPLIANT, NON_COMPLIANT, or EXEMPTED",
				Computed:    true,
			},
		},
	}
}

func (d *WorkspaceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state WorkspaceDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	orgs, err := d.client.Organizations.List(ctx, &terrakube.ListOptions{Filter: "name==" + state.Organization.ValueString()})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading organization", err)
		return
	}

	if len(orgs) == 0 {
		resp.Diagnostics.AddError(fmt.Sprintf("Organization %s not found!", state.Organization.String()), state.Organization.String())
		return
	}

	for _, organization := range orgs {
		state.OrganizationID = types.StringValue(organization.ID)
		state.ID = types.StringValue(organization.ID)
		state.Description = types.StringPointerValue(organization.Description)
	}

	// now try to find the workspace
	workspaces, err := d.client.Workspaces.List(ctx, state.OrganizationID.ValueString(), &terrakube.ListOptions{Filter: "name==" + state.Name.ValueString()})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace", err)
		return
	}

	if len(workspaces) == 0 {
		resp.Diagnostics.AddError(fmt.Sprintf("Workspace %s not found!", state.Name.String()), state.Name.String())
		return
	}

	for _, data := range workspaces {
		state.ID = types.StringValue(data.ID)
		state.Description = types.StringPointerValue(data.Description)
		state.Source = types.StringValue(data.Source)
		state.Branch = types.StringValue(data.Branch)
		state.Folder = types.StringValue(data.Folder)
		state.TemplateID = types.StringValue(data.TemplateID)
		state.IaCType = types.StringValue(data.IaCType)
		state.IaCVersion = types.StringValue(data.IaCVersion)
		state.ExecutionMode = types.StringValue(data.ExecutionMode)
		state.Deleted = types.BoolValue(data.Deleted)
		state.RestrictApplyToConfiguredBranch = types.BoolPointerValue(data.RestrictApplyToConfiguredBranch)
		state.RequireApproveToApply = types.BoolPointerValue(data.RequireApproveToApply)
		state.AllowRemoteApply = types.BoolValue(data.AllowRemoteApply)
		if data.Vcs != nil {
			state.VCSID = types.StringValue(data.Vcs.ID)
		}
		if data.SSH != nil {
			state.SSHID = types.StringValue(data.SSH.ID)
		}
		state.ModuleSshKey = types.StringPointerValue(data.ModuleSSHKey)
		state.PolicyComplianceStatus = types.StringValue(workspacePolicyComplianceStatus(data))
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
