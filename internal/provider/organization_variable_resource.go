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
var _ resource.Resource = &OrganizationVariableResource{}
var _ resource.ResourceWithImportState = &OrganizationVariableResource{}
var _ resource.ResourceWithConfigValidators = &OrganizationVariableResource{}
var _ resource.ResourceWithValidateConfig = &OrganizationVariableResource{}

type OrganizationVariableResource struct {
	client *terrakube.Client
}

type OrganizationVariableResourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Key            types.String `tfsdk:"key"`
	Value          types.String `tfsdk:"value"`
	ValueWo        types.String `tfsdk:"value_wo"`
	ValueWoVersion types.Int64  `tfsdk:"value_wo_version"`
	Description    types.String `tfsdk:"description"`
	Category       types.String `tfsdk:"category"`
	Sensitive      types.Bool   `tfsdk:"sensitive"`
	Hcl            types.Bool   `tfsdk:"hcl"`
}

func NewOrganizationVariableResource() resource.Resource {
	return &OrganizationVariableResource{}
}

func (r *OrganizationVariableResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_variable"
}

func (r *OrganizationVariableResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create an organization variable that can be used by all workspaces inside the organization.",

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

func (r *OrganizationVariableResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return valueWoConfigValidators()
}

func (r *OrganizationVariableResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	resp.Diagnostics.Append(validateValueWoSensitive(ctx, req.Config, "organization variable")...)
}

func (r *OrganizationVariableResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Organization Variable Resource")
}

func (r *OrganizationVariableResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan OrganizationVariableResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	value, diags := valueOrWriteOnly(ctx, req.Config, plan.Value, plan.Sensitive, "organization variable")
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

	organizationVariable, err := r.client.OrganizationVariables.CreateFields(ctx, plan.OrganizationId.ValueString(), terrakube.Patch{Attributes: attributes})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating organization variable", err)
		return
	}

	if *organizationVariable.Sensitive {
		tflog.Info(ctx, "Variable value is not included in response, keeping the planned value for sensitive=true...")
	} else {
		tflog.Info(ctx, "Variable value is included in response...")
		plan.Value = types.StringValue(organizationVariable.Value)
	}
	plan.ValueWo = types.StringNull()

	plan.Key = types.StringValue(organizationVariable.Key)
	plan.Description = types.StringValue(organizationVariable.Description)
	plan.Category = types.StringValue(organizationVariable.Category)
	plan.Sensitive = types.BoolValue(*organizationVariable.Sensitive)
	plan.Hcl = types.BoolValue(organizationVariable.Hcl)
	plan.ID = types.StringValue(organizationVariable.ID)

	tflog.Info(ctx, "organization variable Resource Created", map[string]any{"success": true})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *OrganizationVariableResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state OrganizationVariableResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organizationVariable, err := r.client.OrganizationVariables.Get(ctx, state.OrganizationId.ValueString(), state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Organization variable not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading organization variable", err)
		return
	}

	if *organizationVariable.Sensitive {
		tflog.Info(ctx, "Variable value is not included in response, keeping the current state value")
	} else {
		tflog.Info(ctx, "Variable value is included in response...")
		state.Value = types.StringValue(organizationVariable.Value)
	}

	state.Key = types.StringValue(organizationVariable.Key)
	state.Description = types.StringValue(organizationVariable.Description)
	state.Category = types.StringValue(organizationVariable.Category)
	state.Sensitive = types.BoolValue(*organizationVariable.Sensitive)
	state.Hcl = types.BoolValue(organizationVariable.Hcl)
	state.ID = types.StringValue(organizationVariable.ID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *OrganizationVariableResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan OrganizationVariableResourceModel
	var state OrganizationVariableResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	value, diags := valueOrWriteOnly(ctx, req.Config, plan.Value, plan.Sensitive, "organization variable")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The value is always sent: a write-only value is null in both plan and state.
	// sensitive is set only on create; the update leaves it out.
	attributes := patchAttributes{"value": value}
	attributes.updateString("key", plan.Key, state.Key)
	attributes.updateString("description", plan.Description, state.Description)
	attributes.updateString("category", plan.Category, state.Category)
	attributes.updateBool("hcl", plan.Hcl, state.Hcl)

	organizationID, id := state.OrganizationId.ValueString(), state.ID.ValueString()
	if _, err := r.client.OrganizationVariables.UpdateFields(ctx, organizationID, id, terrakube.Patch{Attributes: attributes}); patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating organization variable", err)
		return
	}

	organizationVariable, err := r.client.OrganizationVariables.Get(ctx, organizationID, id)
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading organization variable after update", err)
		return
	}

	plan.ID = types.StringValue(id)
	plan.Key = types.StringValue(organizationVariable.Key)

	if *organizationVariable.Sensitive {
		tflog.Info(ctx, "Variable value is not included in response, keeping the planned value for sensitive=true...")
	} else {
		tflog.Info(ctx, "Variable value is included in response...")
		plan.Value = types.StringValue(organizationVariable.Value)
	}
	plan.ValueWo = types.StringNull()

	plan.Description = types.StringValue(organizationVariable.Description)
	plan.Category = types.StringValue(organizationVariable.Category)
	plan.Sensitive = types.BoolValue(*organizationVariable.Sensitive)
	plan.Hcl = types.BoolValue(organizationVariable.Hcl)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *OrganizationVariableResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data OrganizationVariableResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.OrganizationVariables.Delete(ctx, data.OrganizationId.ValueString(), data.ID.ValueString())
	reportDeleteError(&resp.Diagnostics, "deleting organization variable", err)
}

func (r *OrganizationVariableResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
