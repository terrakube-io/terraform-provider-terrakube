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
	client   *http.Client
	endpoint string
	token    string
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
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Policy Attachment Data Source Configure Type",
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
	var targetAttachment *client.PolicyAttachmentEntity

	if hasID {
		attachID := state.ID.ValueString()
		url := fmt.Sprintf("%s/api/v1/policy_set/%s/attachments/%s", d.endpoint, policySetID, attachID)
		readReq, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			resp.Diagnostics.AddError("Error creating policy attachment read request", fmt.Sprintf("Error: %s", err))
			return
		}
		readReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
		readReq.Header.Add("Content-Type", "application/vnd.api+json")

		readResp, err := d.client.Do(readReq)
		if err != nil {
			resp.Diagnostics.AddError("Error executing policy attachment read request", fmt.Sprintf("Error: %s", err))
			return
		}
		defer readResp.Body.Close()

		bodyResponse, err := io.ReadAll(readResp.Body)
		if err != nil {
			resp.Diagnostics.AddError("Error reading policy attachment response body", fmt.Sprintf("Error: %s", err))
			return
		}

		if readResp.StatusCode >= http.StatusBadRequest {
			resp.Diagnostics.AddError("Policy Attachment Not Found", fmt.Sprintf("Attachment ID %s was not found for policy set %s: %s", attachID, policySetID, string(bodyResponse)))
			return
		}

		single := &client.PolicyAttachmentEntity{}
		if err := jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), single); err != nil {
			resp.Diagnostics.AddError("Error unmarshalling policy attachment payload", fmt.Sprintf("Error: %s", err))
			return
		}
		targetAttachment = single
	} else {
		url := fmt.Sprintf("%s/api/v1/policy_set/%s/attachments", d.endpoint, policySetID)
		readReq, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			resp.Diagnostics.AddError("Error creating policy attachment list request", fmt.Sprintf("Error: %s", err))
			return
		}
		readReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
		readReq.Header.Add("Content-Type", "application/vnd.api+json")

		readResp, err := d.client.Do(readReq)
		if err != nil {
			resp.Diagnostics.AddError("Error executing policy attachment list request", fmt.Sprintf("Error: %s", err))
			return
		}
		defer readResp.Body.Close()

		bodyResponse, err := io.ReadAll(readResp.Body)
		if err != nil {
			resp.Diagnostics.AddError("Error reading policy attachment list response", fmt.Sprintf("Error: %s", err))
			return
		}

		if readResp.StatusCode >= http.StatusBadRequest {
			resp.Diagnostics.AddError("Error querying policy attachments", fmt.Sprintf("status: %v, body: %v", readResp.Status, string(bodyResponse)))
			return
		}

		rawAttachments, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(bodyResponse)), reflect.TypeOf(new(client.PolicyAttachmentEntity)))
		if err != nil {
			resp.Diagnostics.AddError("Error unmarshalling policy attachments", fmt.Sprintf("Error: %s", err))
			return
		}

		for _, item := range rawAttachments {
			att, ok := item.(*client.PolicyAttachmentEntity)
			if !ok {
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
