package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	terrakube "github.com/terrakube-io/terrakube-go"
)

var (
	_ datasource.DataSource              = &PolicyAttachmentDataSource{}
	_ datasource.DataSourceWithConfigure = &PolicyAttachmentDataSource{}
)

type PolicyAttachmentDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	PolicySetId types.String `tfsdk:"policy_set_id"`
	WorkspaceId types.String `tfsdk:"workspace_id"`
	ProjectId   types.String `tfsdk:"project_id"`
	TagId       types.String `tfsdk:"tag_id"`
}

type PolicyAttachmentDataSource struct {
	client *terrakube.Client
}

func NewPolicyAttachmentDataSource() datasource.DataSource {
	return &PolicyAttachmentDataSource{}
}

func (d *PolicyAttachmentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_policy_attachment"
}

func (d *PolicyAttachmentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Query a Terrakube OPA Policy Attachment by policy set ID and target scope (workspace_id, project_id, tag_id) or attachment ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Attachment ID (UUID). Specify either id or a target scope.",
			},
			"policy_set_id": schema.StringAttribute{
				Required:    true,
				Description: "Target Policy Set ID",
			},
			"workspace_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Attached Workspace ID",
			},
			"project_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Attached Project ID",
			},
			"tag_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Attached Organization Tag ID",
			},
		},
	}
}

func (d *PolicyAttachmentDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Policy Attachment Data Source")
}

func (d *PolicyAttachmentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state PolicyAttachmentDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasID := !state.ID.IsNull() && state.ID.ValueString() != ""
	hasWS := !state.WorkspaceId.IsNull() && state.WorkspaceId.ValueString() != ""
	hasProj := !state.ProjectId.IsNull() && state.ProjectId.ValueString() != ""
	hasTag := !state.TagId.IsNull() && state.TagId.ValueString() != ""

	if !hasID && !hasWS && !hasProj && !hasTag {
		resp.Diagnostics.AddError(
			"Missing Search Criteria",
			"At least one of 'id', 'workspace_id', 'project_id', or 'tag_id' must be specified to query a policy attachment.",
		)
		return
	}

	policySetID := state.PolicySetId.ValueString()
	var targetAttachment *terrakube.PolicyAttachment

	if hasID {
		attachID := state.ID.ValueString()
		single, err := d.client.PolicyAttachments.GetByPolicySet(ctx, policySetID, attachID)
		if err != nil {
			var apiErr *terrakube.APIError
			if errors.As(err, &apiErr) {
				resp.Diagnostics.AddError("Policy Attachment Not Found", fmt.Sprintf("Attachment ID %s was not found for policy set %s: %s", attachID, policySetID, string(apiErr.Body)))
				return
			}
			reportClientError(&resp.Diagnostics, "reading policy attachment", err)
			return
		}
		targetAttachment = single
	} else {
		attachments, err := d.client.PolicyAttachments.ListByPolicySet(ctx, policySetID, nil)
		if err != nil {
			reportClientError(&resp.Diagnostics, "querying policy attachments", err)
			return
		}

		for _, att := range attachments {
			if att == nil {
				continue
			}
			if hasWS && att.Workspace != nil && att.Workspace.ID == state.WorkspaceId.ValueString() {
				targetAttachment = att
				break
			}
			if hasProj && att.Project != nil && att.Project.ID == state.ProjectId.ValueString() {
				targetAttachment = att
				break
			}
			if hasTag && att.Tag != nil && att.Tag.ID == state.TagId.ValueString() {
				targetAttachment = att
				break
			}
		}

		if targetAttachment == nil {
			resp.Diagnostics.AddError(
				"Policy Attachment Not Found",
				fmt.Sprintf("No policy attachment matching the specified scope was found on policy set %q.", policySetID),
			)
			return
		}
	}

	state.ID = types.StringValue(targetAttachment.ID)
	if targetAttachment.Workspace != nil {
		state.WorkspaceId = types.StringValue(targetAttachment.Workspace.ID)
	} else {
		state.WorkspaceId = types.StringNull()
	}
	if targetAttachment.Project != nil {
		state.ProjectId = types.StringValue(targetAttachment.Project.ID)
	} else {
		state.ProjectId = types.StringNull()
	}
	if targetAttachment.Tag != nil {
		state.TagId = types.StringValue(targetAttachment.Tag.ID)
	} else {
		state.TagId = types.StringNull()
	}

	tflog.Debug(ctx, "Policy attachment data source resolved", map[string]any{"id": targetAttachment.ID, "policySetId": policySetID})
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
