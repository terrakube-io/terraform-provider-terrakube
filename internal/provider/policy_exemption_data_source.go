package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	terrakube "github.com/terrakube-io/terrakube-go"
)

var (
	_ datasource.DataSource              = &PolicyExemptionDataSource{}
	_ datasource.DataSourceWithConfigure = &PolicyExemptionDataSource{}
)

type PolicyExemptionDataSourceModel struct {
	ID              types.String `tfsdk:"id"`
	Organization    types.String `tfsdk:"organization"`
	OrganizationId  types.String `tfsdk:"organization_id"`
	PolicySetId     types.String `tfsdk:"policy_set_id"`
	RuleId          types.String `tfsdk:"rule_id"`
	TicketReference types.String `tfsdk:"ticket_reference"`
	Justification   types.String `tfsdk:"justification"`
	ExpiresAt       types.String `tfsdk:"expires_at"`
	WorkspaceId     types.String `tfsdk:"workspace_id"`
	ProjectId       types.String `tfsdk:"project_id"`
}

type PolicyExemptionDataSource struct {
	client *terrakube.Client
}

func NewPolicyExemptionDataSource() datasource.DataSource {
	return &PolicyExemptionDataSource{}
}

func (d *PolicyExemptionDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_policy_exemption"
}

func (d *PolicyExemptionDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Query a Terrakube OPA Policy Exemption by organization name and exemption ID (or rule ID).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Exemption ID (UUID). Specify either id or rule_id.",
			},
			"organization": schema.StringAttribute{
				Required:    true,
				Description: "Organization Name",
			},
			"organization_id": schema.StringAttribute{
				Computed:    true,
				Description: "Organization ID (UUID)",
			},
			"policy_set_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Policy Set ID (UUID)",
			},
			"rule_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "OPA Rego Rule ID",
			},
			"ticket_reference": schema.StringAttribute{
				Computed:    true,
				Description: "Ticket reference (e.g. Jira SEC-8842)",
			},
			"justification": schema.StringAttribute{
				Computed:    true,
				Description: "Exemption justification",
			},
			"expires_at": schema.StringAttribute{
				Computed:    true,
				Description: "Expiration timestamp in RFC3339 format",
			},
			"workspace_id": schema.StringAttribute{
				Computed:    true,
				Description: "Scoped workspace ID if workspace-specific",
			},
			"project_id": schema.StringAttribute{
				Computed:    true,
				Description: "Scoped project ID if project-specific",
			},
		},
	}
}

func (d *PolicyExemptionDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Policy Exemption Data Source")
}

func (d *PolicyExemptionDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state PolicyExemptionDataSourceModel
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

	var targetExemption *terrakube.PolicyExemption

	switch {
	case !state.ID.IsNull() && state.ID.ValueString() != "":
		// Lookup by ID
		single, err := d.client.PolicyExemptions.GetByID(ctx, state.ID.ValueString())
		if err != nil {
			var apiErr *terrakube.APIError
			if errors.As(err, &apiErr) {
				resp.Diagnostics.AddError("Policy Exemption Not Found", fmt.Sprintf("Policy exemption ID %s was not found.", state.ID.ValueString()))
				return
			}
			reportClientError(&resp.Diagnostics, "reading policy exemption", err)
			return
		}
		targetExemption = single
	case !state.RuleId.IsNull() && state.RuleId.ValueString() != "":
		// Lookup by Rule ID in Org
		filter := "ruleId==" + state.RuleId.ValueString()
		if !state.PolicySetId.IsNull() && state.PolicySetId.ValueString() != "" {
			filter = fmt.Sprintf("%s;policySet.id==%s", filter, state.PolicySetId.ValueString())
		}

		exemptions, err := d.client.PolicyExemptions.List(ctx, orgData.ID, &terrakube.ListOptions{Filter: filter})
		if err != nil {
			reportClientError(&resp.Diagnostics, "reading policy exemption", err)
			return
		}
		if len(exemptions) == 0 {
			resp.Diagnostics.AddError("Policy Exemption Not Found", fmt.Sprintf("Policy exemption for rule %s was not found in organization %s.", state.RuleId.ValueString(), state.Organization.ValueString()))
			return
		}
		targetExemption = exemptions[0]
	default:
		resp.Diagnostics.AddError("Missing Filter Parameter", "Either 'id' or 'rule_id' must be specified to query a policy exemption.")
		return
	}

	state.ID = types.StringValue(targetExemption.ID)
	state.RuleId = types.StringValue(stringOrEmpty(targetExemption.RuleID))
	state.TicketReference = types.StringValue(stringOrEmpty(targetExemption.TicketReference))
	state.Justification = types.StringValue(stringOrEmpty(targetExemption.Justification))
	state.ExpiresAt = expiresAtState(types.StringNull(), targetExemption.ExpiresAt)
	if targetExemption.PolicySet != nil {
		state.PolicySetId = types.StringValue(targetExemption.PolicySet.ID)
	} else {
		state.PolicySetId = types.StringNull()
	}
	if targetExemption.Workspace != nil {
		state.WorkspaceId = types.StringValue(targetExemption.Workspace.ID)
	} else {
		state.WorkspaceId = types.StringNull()
	}
	if targetExemption.Project != nil {
		state.ProjectId = types.StringValue(targetExemption.Project.ID)
	} else {
		state.ProjectId = types.StringNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
