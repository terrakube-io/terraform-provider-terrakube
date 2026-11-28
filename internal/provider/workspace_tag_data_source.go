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
	_ datasource.DataSource              = &WorkspaceTagDataSource{}
	_ datasource.DataSourceWithConfigure = &WorkspaceTagDataSource{}
)

type WorkspaceTagDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	OrganizationId types.String `tfsdk:"organization_id"`
	WorkspaceId    types.String `tfsdk:"workspace_id"`
	TagID          types.String `tfsdk:"tag_id"`
	Value          types.String `tfsdk:"value"`
}

type WorkspaceTagDataSource struct {
	client *terrakube.Client
}

func NewWorkspaceTagDataSource() datasource.DataSource {
	return &WorkspaceTagDataSource{}
}

func (d *WorkspaceTagDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_tag"
}

func (d *WorkspaceTagDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Query a tag attached to a Terrakube workspace by the tag name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Workspace Tag Id",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "The name of the tag",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization id",
			},
			"workspace_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube workspace id",
			},
			"tag_id": schema.StringAttribute{
				Computed:    true,
				Description: "Organization Tag Id",
			},
			"value": schema.StringAttribute{
				Computed:    true,
				Description: "Value of a key/value tag (Terrakube 2.34.0+). Null for a key-only tag, or on older Terrakube versions.",
			},
		},
	}
}

func (d *WorkspaceTagDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Workspace Tag Data Source")
}

func (d *WorkspaceTagDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state WorkspaceTagDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organizationId := state.OrganizationId.ValueString()
	workspaceId := state.WorkspaceId.ValueString()
	name := state.Name.ValueString()

	// 1. Resolve the organization tag id from the tag name
	tags, err := d.client.Tags.List(ctx, organizationId, &terrakube.ListOptions{Filter: "name==" + rsqlQuote(name)})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading organization tag", err)
		return
	}

	tagId := ""
	for _, data := range tags {
		if data != nil && data.Name == name {
			tagId = data.ID
			break
		}
	}
	if tagId == "" {
		resp.Diagnostics.AddError("Tag Not Found", fmt.Sprintf("Tag %s was not found in organization %s.", name, organizationId))
		return
	}

	// 2. Find the workspace tag that references the organization tag.
	// Filtering is done client side so it does not depend on server side filter support.
	workspaceTags, err := d.client.WorkspaceTags.List(ctx, organizationId, workspaceId, nil)
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace tag", err)
		return
	}

	for _, data := range workspaceTags {
		if data != nil && data.TagID == tagId {
			state.ID = types.StringValue(data.ID)
			state.TagID = types.StringValue(data.TagID)
			state.Value = workspaceTagValue(data.Value)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}

	resp.Diagnostics.AddError("Workspace Tag Not Found", fmt.Sprintf("Tag %s is not attached to workspace %s.", name, workspaceId))
}
