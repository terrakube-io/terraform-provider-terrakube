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
	client   *http.Client
	endpoint string
	token    string
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

func (d *PolicyExemptionDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, res *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		res.Diagnostics.AddError(
			"Unexpected Policy Exemption Data Source Configure Type",
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

	tflog.Info(ctx, "Configured Policy Exemption datasource", map[string]any{"success": true})
}

func (d *PolicyExemptionDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state PolicyExemptionDataSourceModel
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

	var targetExemption *client.PolicyExemptionEntity

	if !state.ID.IsNull() && state.ID.ValueString() != "" {
		// Lookup by ID
		exReq, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/policy_exemption/%s", d.endpoint, state.ID.ValueString()), nil)
		if err != nil {
			resp.Diagnostics.AddError("Error creating policy exemption request", fmt.Sprintf("Error: %s", err))
			return
		}
		exReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
		exReq.Header.Add("Content-Type", "application/vnd.api+json")

		exResp, err := d.client.Do(exReq)
		if err != nil {
			resp.Diagnostics.AddError("Error executing policy exemption request", fmt.Sprintf("Error: %s", err))
			return
		}
		defer exResp.Body.Close()

		exBody, err := io.ReadAll(exResp.Body)
		if err != nil {
			resp.Diagnostics.AddError("Error reading policy exemption response", fmt.Sprintf("Error: %s", err))
			return
		}

		if exResp.StatusCode >= http.StatusBadRequest {
			resp.Diagnostics.AddError("Policy Exemption Not Found", fmt.Sprintf("Policy exemption ID %s was not found.", state.ID.ValueString()))
			return
		}

		single := &client.PolicyExemptionEntity{}
		if err := jsonapi.UnmarshalPayload(strings.NewReader(string(exBody)), single); err != nil {
			resp.Diagnostics.AddError("Error unmarshalling policy exemption response", fmt.Sprintf("Error: %s", err))
			return
		}
		targetExemption = single
	} else if !state.RuleId.IsNull() && state.RuleId.ValueString() != "" {
		// Lookup by Rule ID in Org
		queryUrl := fmt.Sprintf("%s/api/v1/organization/%s/policyExemption?filter[policy_exemption]=ruleId==%s", d.endpoint, orgData.ID, state.RuleId.ValueString())
		if !state.PolicySetId.IsNull() && state.PolicySetId.ValueString() != "" {
			queryUrl = fmt.Sprintf("%s;policySet.id==%s", queryUrl, state.PolicySetId.ValueString())
		}

		exReq, err := http.NewRequest(http.MethodGet, queryUrl, nil)
		if err != nil {
			resp.Diagnostics.AddError("Error creating policy exemption query request", fmt.Sprintf("Error: %s", err))
			return
		}
		exReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
		exReq.Header.Add("Content-Type", "application/vnd.api+json")

		exResp, err := d.client.Do(exReq)
		if err != nil {
			resp.Diagnostics.AddError("Error executing policy exemption query request", fmt.Sprintf("Error: %s", err))
			return
		}
		defer exResp.Body.Close()

		exBody, err := io.ReadAll(exResp.Body)
		if err != nil {
			resp.Diagnostics.AddError("Error reading policy exemption query response", fmt.Sprintf("Error: %s", err))
			return
		}

		exemptions, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(exBody)), reflect.TypeOf(new(client.PolicyExemptionEntity)))
		if err != nil || len(exemptions) == 0 {
			resp.Diagnostics.AddError("Policy Exemption Not Found", fmt.Sprintf("Policy exemption for rule %s was not found in organization %s.", state.RuleId.ValueString(), state.Organization.ValueString()))
			return
		}
		data, _ := exemptions[0].(*client.PolicyExemptionEntity)
		targetExemption = data
	} else {
		resp.Diagnostics.AddError("Missing Filter Parameter", "Either 'id' or 'rule_id' must be specified to query a policy exemption.")
		return
	}

	state.ID = types.StringValue(targetExemption.ID)
	state.RuleId = types.StringValue(targetExemption.RuleId)
	state.TicketReference = types.StringValue(targetExemption.TicketReference)
	state.Justification = types.StringValue(targetExemption.Justification)
	state.ExpiresAt = types.StringPointerValue(targetExemption.ExpiresAt)
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
