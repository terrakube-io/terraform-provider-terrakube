package provider

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
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
	client   *http.Client
	endpoint string
	token    string
}

// workspaceRunTriggerListPayload is the JSON:API collection response for
// /api/v1/runTrigger. It reuses the resource's single-entry types.
type workspaceRunTriggerListPayload struct {
	Data []workspaceRunTriggerData `json:"data"`
}

func NewWorkspaceRunTriggerDataSource() datasource.DataSource {
	return &WorkspaceRunTriggerDataSource{}
}

func (d *WorkspaceRunTriggerDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Workspace Run Trigger Data Source Configure Type",
			fmt.Sprintf("Expected *TerrakubeConnectionData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	d.client = &http.Client{}
	if providerData.InsecureHttpClient {
		if custom, ok := http.DefaultTransport.(*http.Transport); ok {
			customTransport := custom.Clone()
			customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
			d.client = &http.Client{Transport: customTransport}
		}
	}
	d.endpoint = providerData.Endpoint
	d.token = providerData.Token
	tflog.Debug(ctx, "Configured Workspace Run Trigger data source")
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
	filter := fmt.Sprintf("sourceWorkspace.id==%s;destinationWorkspace.id==%s", rsqlQuote(source), rsqlQuote(destination))
	reqURL := fmt.Sprintf("%s/api/v1/runTrigger?filter[runTrigger]=%s", d.endpoint, url.QueryEscape(filter))

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace run trigger data source request", fmt.Sprintf("Error creating request: %s", err))
		return
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", d.token))
	request.Header.Add("Accept", "application/vnd.api+json")

	response, err := d.client.Do(request)
	if err != nil {
		resp.Diagnostics.AddError("Error executing workspace run trigger request", fmt.Sprintf("Error executing request: %s", err))
		return
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading workspace run trigger response body", fmt.Sprintf("Error reading response body: %s", err))
		return
	}
	if reportAPIErrorStatus(&resp.Diagnostics, "reading workspace run trigger", response, body) {
		return
	}

	var payload workspaceRunTriggerListPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		resp.Diagnostics.AddError("Unable to unmarshal workspace run trigger payload", fmt.Sprintf("Unable to unmarshal payload: %s", err))
		return
	}

	// The API filters on both workspaces; check again so a server that ignores
	// part of the filter cannot return the wrong trigger.
	var found *workspaceRunTriggerData
	for i := range payload.Data {
		trigger := &payload.Data[i]
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
		resp.Diagnostics.AddError("Workspace run trigger not found",
			fmt.Sprintf("No run trigger from workspace %s to workspace %s was found.", source, destination))
		return
	}

	state.ID = types.StringValue(found.ID)
	state.Enabled = types.BoolValue(found.Attributes.Enabled)
	state.TemplateID = types.StringNull()
	if found.Relationships.Template.Data != nil {
		state.TemplateID = types.StringValue(found.Relationships.Template.Data.ID)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func runTriggerLinks(trigger *workspaceRunTriggerData, source, destination string) bool {
	rel := trigger.Relationships
	return rel.SourceWorkspace.Data != nil && rel.SourceWorkspace.Data.ID == source &&
		rel.DestinationWorkspace.Data != nil && rel.DestinationWorkspace.Data.ID == destination
}
