package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
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
var _ resource.Resource = &OrganizationTemplateResource{}
var _ resource.ResourceWithImportState = &OrganizationTemplateResource{}

type OrganizationTemplateResource struct {
	client *terrakube.Client
}

type OrganizationTemplateResourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	Version        types.String `tfsdk:"version"`
	Content        types.String `tfsdk:"content"`
}

func NewOrganizationTemplateResource() resource.Resource {
	return &OrganizationTemplateResource{}
}

func (r *OrganizationTemplateResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_template"
}

func (r *OrganizationTemplateResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a template and attach it to your organization. This can be useful to define new" +
			"process like drift detection, cost mapping, etc...",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Template Id",
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
				Description: "The name of the template",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "The description of the template",
			},
			"version": schema.StringAttribute{
				Optional:    true,
				Description: "The version of the template",
			},
			"content": schema.StringAttribute{
				Required:    true,
				Description: "The content of the template",
			},
		},
	}
}

func (r *OrganizationTemplateResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Organization Template Resource")
}

func (r *OrganizationTemplateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan OrganizationTemplateResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.createString("name", plan.Name)
	attributes.createString("description", plan.Description)
	attributes.createString("version", plan.Version)
	attributes.createString("tcl", encodeTemplateContent(plan.Content))

	template, err := r.client.Templates.CreateFields(ctx, plan.OrganizationId.ValueString(), terrakube.Patch{Attributes: attributes})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating organization template", err)
		return
	}

	plan.ID = types.StringValue(template.ID)
	if !setOrganizationTemplateState(&plan, template, &resp.Diagnostics) {
		return
	}

	tflog.Info(ctx, "Organization Template Resource Created", map[string]any{"success": true})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *OrganizationTemplateResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state OrganizationTemplateResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	template, err := r.client.Templates.Get(ctx, state.OrganizationId.ValueString(), state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Organization template not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading organization template", err)
		return
	}

	if !setOrganizationTemplateState(&state, template, &resp.Diagnostics) {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *OrganizationTemplateResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan OrganizationTemplateResourceModel
	var state OrganizationTemplateResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.updateString("name", plan.Name, state.Name)
	attributes.updateString("description", plan.Description, state.Description)
	attributes.updateString("version", plan.Version, state.Version)
	attributes.updateString("tcl", encodeTemplateContent(plan.Content), encodeTemplateContent(state.Content))

	organizationID, id := state.OrganizationId.ValueString(), state.ID.ValueString()
	template, err := r.client.Templates.UpdateFields(ctx, organizationID, id, terrakube.Patch{Attributes: attributes})
	if patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating organization template", err)
		return
	}

	template, err = readAfterPatch(template, func() (*terrakube.Template, error) {
		return r.client.Templates.Get(ctx, organizationID, id)
	})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading organization template after update", err)
		return
	}

	plan.ID = types.StringValue(id)
	if !setOrganizationTemplateState(&plan, template, &resp.Diagnostics) {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *OrganizationTemplateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data OrganizationTemplateResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Templates.Delete(ctx, data.OrganizationId.ValueString(), data.ID.ValueString())
	if err != nil && !terrakube.IsNotFound(err) {
		reportClientError(&resp.Diagnostics, "deleting organization template", err)
	}
}

func (r *OrganizationTemplateResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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

// encodeTemplateContent returns the template flow base64 encoded, the way Terrakube stores it in
// the tcl attribute. A null or unknown content stays null or unknown.
func encodeTemplateContent(content types.String) types.String {
	if content.IsNull() || content.IsUnknown() {
		return content
	}
	return types.StringValue(base64.StdEncoding.EncodeToString([]byte(content.ValueString())))
}

// setOrganizationTemplateState copies the attributes Terrakube returned into the model and
// decodes the template flow. It reports false, with a diagnostic, when the flow is not base64.
func setOrganizationTemplateState(model *OrganizationTemplateResourceModel, template *terrakube.Template, diags *diag.Diagnostics) bool {
	content, err := base64.StdEncoding.DecodeString(template.Content)
	if err != nil {
		diags.AddError("Error decoding the content from Base64.", fmt.Sprintf("Error decode the tcl: %s", err))
		return false
	}

	model.Name = types.StringValue(template.Name)
	model.Description = types.StringPointerValue(template.Description)
	model.Version = types.StringPointerValue(template.Version)
	model.Content = types.StringValue(string(content))
	return true
}
