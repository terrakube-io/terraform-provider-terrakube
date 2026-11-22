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
	_ datasource.DataSource              = &CollectionItemDataSource{}
	_ datasource.DataSourceWithConfigure = &CollectionItemDataSource{}
)

type CollectionItemDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	CollectionId   types.String `tfsdk:"collection_id"`
	Key            types.String `tfsdk:"key"`
	Category       types.String `tfsdk:"category"`
	Value          types.String `tfsdk:"value"`
	Description    types.String `tfsdk:"description"`
	Sensitive      types.Bool   `tfsdk:"sensitive"`
	Hcl            types.Bool   `tfsdk:"hcl"`
}

type CollectionItemDataSource struct {
	client *terrakube.Client
}

func NewCollectionItemDataSource() datasource.DataSource {
	return &CollectionItemDataSource{}
}

func (d *CollectionItemDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Collection Item Data Source")
}

func (d *CollectionItemDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_collection_item"
}

func (d *CollectionItemDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a collection item by key within a Terrakube collection. The same key can exist in more than one category, so set `category` when it does. `value` is null for sensitive items, and the attribute is always marked sensitive.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Collection item ID",
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization ID",
			},
			"collection_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube collection ID",
			},
			"key": schema.StringAttribute{
				Required:    true,
				Description: "Item key",
			},
			"category": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Item category, ENV or TERRAFORM. Required when the key exists in both.",
			},
			"value": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "Item value. Null when the item is sensitive.",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Item description",
			},
			"sensitive": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the item is sensitive",
			},
			"hcl": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the value is HCL",
			},
		},
	}
}

func (d *CollectionItemDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state CollectionItemDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key := state.Key.ValueString()
	category := state.Category.ValueString()
	filter := "key==" + rsqlQuote(key)
	if category != "" {
		filter += ";category==" + rsqlQuote(category)
	}
	entities, err := d.client.CollectionItems.List(ctx, state.OrganizationId.ValueString(), state.CollectionId.ValueString(), &terrakube.ListOptions{Filter: filter})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading collection item", err)
		return
	}

	var found *terrakube.CollectionItem
	for _, entity := range entities {
		if entity == nil || entity.Key != key || (category != "" && entity.Category != category) {
			continue
		}
		if found != nil {
			resp.Diagnostics.AddError("Multiple collection items found",
				fmt.Sprintf("More than one item with key %q was found in collection %s of organization %s. Set category to choose one.", key, state.CollectionId.ValueString(), state.OrganizationId.ValueString()))
			return
		}
		found = entity
	}
	if found == nil {
		resp.Diagnostics.AddError("Collection item not found",
			fmt.Sprintf("No item with key %q was found in collection %s of organization %s.", key, state.CollectionId.ValueString(), state.OrganizationId.ValueString()))
		return
	}

	sensitive := found.Sensitive
	state.ID = types.StringValue(found.ID)
	state.Category = types.StringValue(found.Category)
	state.Description = types.StringPointerValue(found.Description)
	state.Sensitive = types.BoolValue(sensitive)
	state.Hcl = types.BoolValue(found.Hcl)
	state.Value = types.StringNull()
	if !sensitive {
		state.Value = types.StringValue(found.Value)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
