package provider

import (
	"context"
	"errors"
	"fmt"
	"regexp"
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
var _ resource.Resource = &CollectionItemResource{}
var _ resource.ResourceWithImportState = &CollectionItemResource{}
var _ resource.ResourceWithConfigValidators = &CollectionItemResource{}
var _ resource.ResourceWithValidateConfig = &CollectionItemResource{}

type CollectionItemResource struct {
	client *terrakube.Client
}

// unknownCollectionItemPattern matches the error Terrakube returns for an item id it does not know.
var unknownCollectionItemPattern = regexp.MustCompile(`Unknown identifier .* for item`)

type CollectionItemResourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	CollectionId   types.String `tfsdk:"collection_id"`
	Key            types.String `tfsdk:"key"`
	Value          types.String `tfsdk:"value"`
	ValueWo        types.String `tfsdk:"value_wo"`
	ValueWoVersion types.Int64  `tfsdk:"value_wo_version"`
	Description    types.String `tfsdk:"description"`
	Category       types.String `tfsdk:"category"`
	Sensitive      types.Bool   `tfsdk:"sensitive"`
	Hcl            types.Bool   `tfsdk:"hcl"`
}

func NewCollectionItemResource() resource.Resource {
	return &CollectionItemResource{}
}

func (r *CollectionItemResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_collection_item"
}

func (r *CollectionItemResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create collection item that will be used by this workspace only.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Collection Id",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization id",
			},
			"collection_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube collection id",
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
				Optional:    true,
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

func (r *CollectionItemResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return valueWoConfigValidators()
}

func (r *CollectionItemResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	resp.Diagnostics.Append(validateValueWoSensitive(ctx, req.Config, "collection item")...)
}

func (r *CollectionItemResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Collection Item Resource")
}

func (r *CollectionItemResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan CollectionItemResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	value, diags := valueOrWriteOnly(ctx, req.Config, plan.Value, plan.Sensitive, "collection item")
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

	collectionItem, err := r.client.CollectionItems.CreateFields(ctx, plan.OrganizationId.ValueString(), plan.CollectionId.ValueString(), terrakube.Patch{Attributes: attributes})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating collection item", err)
		return
	}

	if collectionItem.Sensitive {
		tflog.Info(ctx, "Collection item value is not included in response, keeping the planned value for sensitive=true...")
	} else {
		tflog.Info(ctx, "Collection item is included in response...")
		plan.Value = types.StringValue(collectionItem.Value)
	}
	plan.ValueWo = types.StringNull()

	plan.Key = types.StringValue(collectionItem.Key)
	plan.Description = types.StringPointerValue(collectionItem.Description)
	plan.Category = types.StringValue(collectionItem.Category)
	plan.Sensitive = types.BoolValue(collectionItem.Sensitive)
	plan.Hcl = types.BoolValue(collectionItem.Hcl)
	plan.ID = types.StringValue(collectionItem.ID)

	tflog.Info(ctx, "collection item Resource Created", map[string]any{"success": true})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *CollectionItemResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state CollectionItemResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organizationID, collectionID := state.OrganizationId.ValueString(), state.CollectionId.ValueString()
	collectionItem, err := r.client.CollectionItems.Get(ctx, organizationID, collectionID, state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Collection item not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	var apiErr *terrakube.APIError
	if errors.As(err, &apiErr) && len(apiErr.Errors) > 0 && unknownCollectionItemPattern.MatchString(apiErr.Errors[0].Detail) {
		// This might be a known issue where the item is removed in the UI and added back manually
		// with the same key but a different id, so look it up by key.
		tflog.Debug(ctx, "this might be a known issue where the variable is removed in the gui and added back manually with the same key but a different id")
		items, err := r.client.CollectionItems.List(ctx, organizationID, collectionID, &terrakube.ListOptions{Filter: "key==" + state.Key.ValueString()})
		if err != nil {
			reportClientError(&resp.Diagnostics, "searching collection item by key", err)
			return
		}
		if len(items) == 0 {
			resp.Diagnostics.AddError("Error unmarshal second payload response", fmt.Sprintf("No collection item with the key %q was found", state.Key.ValueString()))
			return
		}
		collectionItem = items[0]
		tflog.Info(ctx, fmt.Sprintf("Successfully found the new id for this variable %s => %s", state.ID.ValueString(), collectionItem.ID))
	} else if err != nil {
		reportClientError(&resp.Diagnostics, "reading collection item", err)
		return
	}

	if collectionItem.Sensitive {
		tflog.Info(ctx, "Collection item value is not included in response, keeping the current state value")
	} else {
		tflog.Info(ctx, "Collection item value is included in response...")
		state.Value = types.StringValue(collectionItem.Value)
	}

	state.Key = types.StringValue(collectionItem.Key)
	state.Description = types.StringPointerValue(collectionItem.Description)
	state.Category = types.StringValue(collectionItem.Category)
	state.Sensitive = types.BoolValue(collectionItem.Sensitive)
	state.Hcl = types.BoolValue(collectionItem.Hcl)
	state.ID = types.StringValue(collectionItem.ID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *CollectionItemResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan CollectionItemResourceModel
	var state CollectionItemResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	value, diags := valueOrWriteOnly(ctx, req.Config, plan.Value, plan.Sensitive, "collection item")
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

	organizationID, collectionID, id := state.OrganizationId.ValueString(), state.CollectionId.ValueString(), state.ID.ValueString()
	if _, err := r.client.CollectionItems.UpdateFields(ctx, organizationID, collectionID, id, terrakube.Patch{Attributes: attributes}); patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating collection item", err)
		return
	}

	collectionItem, err := r.client.CollectionItems.Get(ctx, organizationID, collectionID, id)
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading collection item after update", err)
		return
	}

	if collectionItem.Sensitive {
		// The value is not in the response: keep the planned one (null with value_wo).
		tflog.Info(ctx, "Collection item is not included in response, keeping the planned value for sensitive=true...")
	} else {
		tflog.Info(ctx, "Collection value is included in response...")
		plan.Value = types.StringValue(collectionItem.Value)
	}
	plan.ValueWo = types.StringNull()

	plan.ID = types.StringValue(id)
	plan.Key = types.StringValue(collectionItem.Key)
	plan.Description = types.StringPointerValue(collectionItem.Description)
	plan.Category = types.StringValue(collectionItem.Category)
	plan.Sensitive = types.BoolValue(collectionItem.Sensitive)
	plan.Hcl = types.BoolValue(collectionItem.Hcl)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *CollectionItemResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data CollectionItemResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.CollectionItems.Delete(ctx, data.OrganizationId.ValueString(), data.CollectionId.ValueString(), data.ID.ValueString())
	if err != nil && !terrakube.IsNotFound(err) {
		reportClientError(&resp.Diagnostics, "deleting collection item", err)
	}
}

func (r *CollectionItemResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) != 3 || idParts[0] == "" || idParts[1] == "" || idParts[2] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: 'organization_ID,collection_ID, ID', Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("collection_id"), idParts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[2])...)
}
