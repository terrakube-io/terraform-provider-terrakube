package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	terrakube "github.com/terrakube-io/terrakube-go"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &ModuleResource{}
var _ resource.ResourceWithImportState = &ModuleResource{}

type ModuleResource struct {
	client *terrakube.Client
}

type ModuleResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Description    types.String `tfsdk:"description"`
	ProviderName   types.String `tfsdk:"provider_name"`
	Source         types.String `tfsdk:"source"`
	VcsId          types.String `tfsdk:"vcs_id"`
	SshId          types.String `tfsdk:"ssh_id"`
	TagPrefix      types.String `tfsdk:"tag_prefix"`
	Folder         types.String `tfsdk:"folder"`
}

func NewModuleResource() resource.Resource {
	return &ModuleResource{}
}

func (r *ModuleResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_module"
}

func (r *ModuleResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Resource for managing modules in Terrakube. " +
			"This resource allows you to create, read, update, and delete modules within a specified organization.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Module Id",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization id",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Module name",
			},
			"description": schema.StringAttribute{
				Required:    true,
				Description: "Module description",
			},
			"provider_name": schema.StringAttribute{
				Required:    true,
				Description: "Module provider name. Example: azurerm, google, aws, etc",
			},
			"source": schema.StringAttribute{
				Required:    true,
				Description: "Source repository for the module(git using https or ssh protocol)",
			},
			"vcs_id": schema.StringAttribute{
				Optional:    true,
				Description: "VCS connection ID for private modules",
			},
			"ssh_id": schema.StringAttribute{
				Optional:    true,
				Description: "Ssh connection ID for private modules",
			},
			"tag_prefix": schema.StringAttribute{
				Optional:    true,
				Description: "Prefix tag mono-repository modules. module/ will pick up any tag starting with 'module/*'",
			},
			"folder": schema.StringAttribute{
				Optional:    true,
				Description: "Folder to look into for module files. Need to preprend a / and append a / to work properly.",
			},
		},
	}
}

func (r *ModuleResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Module Resource")
}

func (r *ModuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ModuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.createString("name", plan.Name)
	attributes.createString("description", plan.Description)
	attributes.createString("provider", plan.ProviderName)
	attributes.createString("source", plan.Source)
	attributes.createString("folder", plan.Folder)
	attributes.createString("tagPrefix", plan.TagPrefix)

	patch := terrakube.Patch{Attributes: attributes, Relationships: moduleRelationships(plan)}
	module, err := r.client.Modules.CreateFields(ctx, plan.OrganizationId.ValueString(), patch)
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating module", err)
		return
	}

	plan.ID = types.StringValue(module.ID)
	plan.Name = types.StringValue(module.Name)
	plan.Description = types.StringValue(module.Description)
	plan.ProviderName = types.StringValue(module.Provider)
	plan.Source = types.StringValue(module.Source)

	if module.Folder != nil {
		plan.Folder = types.StringPointerValue(module.Folder)
	}

	if module.TagPrefix != nil {
		plan.TagPrefix = types.StringPointerValue(module.TagPrefix)
	}

	tflog.Info(ctx, "Module Resource Created", map[string]any{"success": true})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ModuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ModuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	module, err := r.client.Modules.Get(ctx, state.OrganizationId.ValueString(), state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Module not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading module", err)
		return
	}

	state.Name = types.StringValue(module.Name)
	state.Description = types.StringValue(module.Description)
	state.ProviderName = types.StringValue(module.Provider)
	state.Source = types.StringValue(module.Source)

	if module.Folder != nil {
		state.Folder = types.StringPointerValue(module.Folder)
	}

	if module.TagPrefix != nil {
		state.TagPrefix = types.StringPointerValue(module.TagPrefix)
	}

	if module.Vcs != nil {
		state.VcsId = types.StringValue(module.Vcs.ID)
	}

	if module.SSH != nil {
		state.SshId = types.StringValue(module.SSH.ID)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ModuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ModuleResourceModel
	var state ModuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.updateString("name", plan.Name, state.Name)
	attributes.updateString("description", plan.Description, state.Description)
	attributes.updateString("provider", plan.ProviderName, state.ProviderName)
	attributes.updateString("source", plan.Source, state.Source)
	attributes.updateString("folder", plan.Folder, state.Folder)
	attributes.updateString("tagPrefix", plan.TagPrefix, state.TagPrefix)

	organizationID, id := state.OrganizationId.ValueString(), state.ID.ValueString()
	patch := terrakube.Patch{Attributes: attributes, Relationships: moduleRelationships(plan)}
	module, err := r.client.Modules.UpdateFields(ctx, organizationID, id, patch)
	if patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating module", err)
		return
	}

	module, err = readAfterPatch(module, func() (*terrakube.Module, error) {
		return r.client.Modules.Get(ctx, organizationID, id)
	})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading module after update", err)
		return
	}

	plan.ID = types.StringValue(id)
	plan.Name = types.StringValue(module.Name)
	plan.Description = types.StringValue(module.Description)
	plan.ProviderName = types.StringValue(module.Provider)
	plan.Source = types.StringValue(module.Source)
	plan.TagPrefix = types.StringPointerValue(module.TagPrefix)

	if module.Folder != nil {
		plan.Folder = types.StringPointerValue(module.Folder)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ModuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ModuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Modules.Delete(ctx, data.OrganizationId.ValueString(), data.ID.ValueString())
	reportDeleteError(&resp.Diagnostics, "deleting module", err)
}

// moduleRelationships links the module to its VCS connection and SSH key when they are set.
// A link is never cleared: a vcs_id or ssh_id removed from the configuration is not sent, and
// Terrakube does not clear a link that is left out.
func moduleRelationships(model ModuleResourceModel) map[string]terrakube.Relationship {
	relationships := map[string]terrakube.Relationship{}
	if !model.VcsId.IsNull() && !model.VcsId.IsUnknown() {
		vcsID := model.VcsId.ValueString()
		relationships["vcs"] = terrakube.Relationship{Type: "vcs", ID: &vcsID}
	}
	if !model.SshId.IsNull() && !model.SshId.IsUnknown() {
		sshID := model.SshId.ValueString()
		relationships["ssh"] = terrakube.Relationship{Type: "ssh", ID: &sshID}
	}
	return relationships
}

// ImportState accepts "organization_id,id": Read needs the organization ID to
// build the module URL, so importing the module ID alone cannot work.
func (r *ModuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) != 2 || idParts[0] == "" || idParts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: 'organization_ID,ID', Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[1])...)
}
