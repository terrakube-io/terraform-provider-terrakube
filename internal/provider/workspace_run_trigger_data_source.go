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
	_ datasource.DataSource              = &WorkspaceRunTriggerDataSource{}
	_ datasource.DataSourceWithConfigure = &WorkspaceRunTriggerDataSource{}
)

type WorkspaceRunTriggerDataSourceModel struct {
	ID                     types.String `tfsdk:"id"`
	SourceWorkspaceID      types.String `tfsdk:"source_workspace_id"`
	DestinationWorkspaceID types.String `tfsdk:"destination_workspace_id"`
	TemplateID             types.String `tfsdk:"template_id"`
	Enabled                types.Bool   `tfsdk:"enabled"`
}

type WorkspaceRunTriggerDataSource struct {
	client *terrakube.Client
}

func NewWorkspaceRunTriggerDataSource() datasource.DataSource {
	return &WorkspaceRunTriggerDataSource{}
}

func (d *WorkspaceRunTriggerDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Workspace Run Trigger Data Source")
}

func (d *WorkspaceRunTriggerDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_run_trigger"
}

func (d *WorkspaceRunTriggerDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up the run trigger between a source and a destination workspace. Requires Terrakube 2.34.0 or later.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Run trigger ID (UUID)",
			},
			"source_workspace_id": schema.StringAttribute{
				Required:    true,
				Description: "ID of the upstream workspace whose successful apply fires the trigger.",
			},
			"destination_workspace_id": schema.StringAttribute{
				Required:    true,
				Description: "ID of the downstream workspace that receives the triggered run.",
			},
			"template_id": schema.StringAttribute{
				Computed:    true,
				Description: "Template ID used for the triggered run. Null when the destination workspace default template is used.",
			},
			"enabled": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether this trigger is active.",
			},
		},
	}
}

func (d *WorkspaceRunTriggerDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state WorkspaceRunTriggerDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	source := state.SourceWorkspaceID.ValueString()
	destination := state.DestinationWorkspaceID.ValueString()
	notFoundDetail := fmt.Sprintf("No run trigger from workspace %s to workspace %s was found.", source, destination)

	// terrakube-go lists run triggers only below a workspace, whose runTriggers are the triggers
	// it is the destination of, and that path needs the workspace's organization.
	workspace, err := d.client.Workspaces.GetByID(ctx, destination)
	if terrakube.IsNotFound(err) {
		resp.Diagnostics.AddError("Workspace run trigger not found", notFoundDetail)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace run trigger", err)
		return
	}
	if workspace.Organization == nil {
		resp.Diagnostics.AddError("Error reading workspace run trigger", fmt.Sprintf("Terrakube returned workspace %s without its organization.", destination))
		return
	}

	filter := fmt.Sprintf("sourceWorkspace.id==%s;destinationWorkspace.id==%s", rsqlQuote(source), rsqlQuote(destination))
	triggers, err := d.client.RunTriggers.List(ctx, workspace.Organization.ID, destination, &terrakube.ListOptions{Filter: filter})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace run trigger", err)
		return
	}

	// The API filters on both workspaces; check again so a server that ignores
	// part of the filter cannot return the wrong trigger.
	var found *terrakube.RunTrigger
	for _, trigger := range triggers {
		if !runTriggerLinks(trigger, source, destination) {
			continue
		}
		if found != nil {
			resp.Diagnostics.AddError("Multiple workspace run triggers found",
				fmt.Sprintf("More than one run trigger from workspace %s to workspace %s was found.", source, destination))
			return
		}
		found = trigger
	}
	if found == nil {
		resp.Diagnostics.AddError("Workspace run trigger not found", notFoundDetail)
		return
	}

	state.ID = types.StringValue(found.ID)
	state.Enabled = types.BoolValue(found.Enabled)
	state.TemplateID = types.StringNull()
	if found.Template != nil {
		state.TemplateID = types.StringValue(found.Template.ID)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func runTriggerLinks(trigger *terrakube.RunTrigger, source, destination string) bool {
	return trigger != nil &&
		trigger.SourceWorkspace != nil && trigger.SourceWorkspace.ID == source &&
		trigger.DestinationWorkspace != nil && trigger.DestinationWorkspace.ID == destination
}
