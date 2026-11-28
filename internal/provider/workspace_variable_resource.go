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
var _ resource.Resource = &WorkspaceVariableResource{}
var _ resource.ResourceWithImportState = &WorkspaceVariableResource{}
var _ resource.ResourceWithConfigValidators = &WorkspaceVariableResource{}
var _ resource.ResourceWithValidateConfig = &WorkspaceVariableResource{}

type WorkspaceVariableResource struct {
	client *terrakube.Client
}

type WorkspaceVariableResourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	WorkspaceId    types.String `tfsdk:"workspace_id"`
	Key            types.String `tfsdk:"key"`
	Value          types.String `tfsdk:"value"`
	ValueWo        types.String `tfsdk:"value_wo"`
	ValueWoVersion types.Int64  `tfsdk:"value_wo_version"`
	Description    types.String `tfsdk:"description"`
	Category       types.String `tfsdk:"category"`
	Sensitive      types.Bool   `tfsdk:"sensitive"`
	Hcl            types.Bool   `tfsdk:"hcl"`
}

func NewWorkspaceVariableResource() resource.Resource {
	return &WorkspaceVariableResource{}
}

func (r *WorkspaceVariableResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_variable"
}

func (r *WorkspaceVariableResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create variables that will be used by this workspace only.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Variable Id",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization id",
			},
			"workspace_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube workspace id",
			},
			"key": schema.StringAttribute{
				Required:    true,
				Description: "Variable key",
			},
			"value": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: valueDescription,
			},
			"value_wo":         valueWoAttribute(),
			"value_wo_version": valueWoVersionAttribute(),
			"description": schema.StringAttribute{
				Required:    true,
				Description: "Variable description",
			},
			"category": schema.StringAttribute{
				Required:    true,
				Description: "Variable category (ENV or TERRAFORM). ENV variables are injected in workspace environment at runtime.",
			},
			"sensitive": schema.BoolAttribute{
				Required:    true,
				Description: "Sensitive variables are never shown in the UI or API. They may appear in Terraform logs if your configuration is designed to output them.",
			},
			"hcl": schema.BoolAttribute{
				Required:    true,
				Description: "Parse this field as HashiCorp Configuration Language (HCL). This allows you to interpolate values at runtime.",
			},
		},
	}
}

func (r *WorkspaceVariableResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return valueWoConfigValidators()
}

func (r *WorkspaceVariableResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	resp.Diagnostics.Append(validateValueWoSensitive(ctx, req.Config, "workspace variable")...)
}

func (r *WorkspaceVariableResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Workspace Variable Resource")
}

func (r *WorkspaceVariableResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceVariableResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	value, diags := valueOrWriteOnly(ctx, req.Config, plan.Value, plan.Sensitive, "workspace variable")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// value comes from value or value_wo, exactly one of which is set, so it is always sent.
	attributes := patchAttributes{"value": value}
	attributes.createString("key", plan.Key)
	attributes.createString("description", plan.Description)
	attributes.createString("category", plan.Category)
	attributes.createBool("sensitive", plan.Sensitive)
	attributes.createBool("hcl", plan.Hcl)

	workspaceVariable, err := r.client.Variables.CreateFields(ctx, plan.OrganizationId.ValueString(), plan.WorkspaceId.ValueString(), terrakube.Patch{Attributes: attributes})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating workspace variable", err)
		return
	}

	if workspaceVariable.Sensitive {
		tflog.Info(ctx, "Variable value is not included in response, keeping the planned value for sensitive=true...")
	} else {
		tflog.Info(ctx, "Variable value is included in response...")
		plan.Value = types.StringValue(workspaceVariable.Value)
	}
	plan.ValueWo = types.StringNull()

	plan.Key = types.StringValue(workspaceVariable.Key)
	plan.Description = types.StringValue(workspaceVariable.Description)
	plan.Category = types.StringValue(workspaceVariable.Category)
	plan.Sensitive = types.BoolValue(workspaceVariable.Sensitive)
	plan.Hcl = types.BoolValue(workspaceVariable.Hcl)
	plan.ID = types.StringValue(workspaceVariable.ID)

	tflog.Info(ctx, "workspace variable Resource Created", map[string]any{"success": true})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceVariableResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceVariableResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceVariable, err := r.client.Variables.Get(ctx, state.OrganizationId.ValueString(), state.WorkspaceId.ValueString(), state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Workspace variable not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace variable", err)
		return
	}

	if workspaceVariable.Sensitive {
		tflog.Info(ctx, "Variable value is not included in response, keeping the current state value")
	} else {
		tflog.Info(ctx, "Variable value is included in response...")
		state.Value = types.StringValue(workspaceVariable.Value)
	}

	state.Key = types.StringValue(workspaceVariable.Key)
	state.Description = types.StringValue(workspaceVariable.Description)
	state.Category = types.StringValue(workspaceVariable.Category)
	state.Sensitive = types.BoolValue(workspaceVariable.Sensitive)
	state.Hcl = types.BoolValue(workspaceVariable.Hcl)
	state.ID = types.StringValue(workspaceVariable.ID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *WorkspaceVariableResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan WorkspaceVariableResourceModel
	var state WorkspaceVariableResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	value, diags := valueOrWriteOnly(ctx, req.Config, plan.Value, plan.Sensitive, "workspace variable")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The value is always sent: a write-only value is null in both plan and state.
	attributes := patchAttributes{"value": value}
	attributes.updateString("key", plan.Key, state.Key)
	attributes.updateString("description", plan.Description, state.Description)
	attributes.updateString("category", plan.Category, state.Category)
	attributes.updateBool("sensitive", plan.Sensitive, state.Sensitive)
	attributes.updateBool("hcl", plan.Hcl, state.Hcl)

	organizationID, workspaceID, id := state.OrganizationId.ValueString(), state.WorkspaceId.ValueString(), state.ID.ValueString()
	if _, err := r.client.Variables.UpdateFields(ctx, organizationID, workspaceID, id, terrakube.Patch{Attributes: attributes}); patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating workspace variable", err)
		return
	}

	workspaceVariable, err := r.client.Variables.Get(ctx, organizationID, workspaceID, id)
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading workspace variable after update", err)
		return
	}

	if workspaceVariable.Sensitive {
		tflog.Info(ctx, "Variable value is not included in response, keeping the planned value for sensitive=true...")
	} else {
		tflog.Info(ctx, "Variable value is included in response...")
		plan.Value = types.StringValue(workspaceVariable.Value)
	}
	plan.ValueWo = types.StringNull()

	plan.ID = types.StringValue(id)
	plan.Key = types.StringValue(workspaceVariable.Key)
	plan.Description = types.StringValue(workspaceVariable.Description)
	plan.Category = types.StringValue(workspaceVariable.Category)
	plan.Sensitive = types.BoolValue(workspaceVariable.Sensitive)
	plan.Hcl = types.BoolValue(workspaceVariable.Hcl)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceVariableResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data WorkspaceVariableResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Variables.Delete(ctx, data.OrganizationId.ValueString(), data.WorkspaceId.ValueString(), data.ID.ValueString())
	if err != nil && !terrakube.IsNotFound(err) {
		reportClientError(&resp.Diagnostics, "deleting workspace variable", err)
	}
}

func (r *WorkspaceVariableResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) != 3 || idParts[0] == "" || idParts[1] == "" || idParts[2] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: 'organization_ID,workspace_ID, ID', Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), idParts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[2])...)
}
