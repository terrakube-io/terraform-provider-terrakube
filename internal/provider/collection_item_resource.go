package provider

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"terraform-provider-terrakube/internal/client"

	"github.com/google/jsonapi"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &CollectionItemResource{}
var _ resource.ResourceWithImportState = &CollectionItemResource{}

type CollectionItemResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type CollectionItemResourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	CollectionId   types.String `tfsdk:"collection_id"`
	Key            types.String `tfsdk:"key"`
	Value          types.String `tfsdk:"value"`
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
				Required:    true,
				Sensitive:   true,
				Description: "Variable value",
			},
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

func (r *CollectionItemResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Collection Item Resource Configure Type",
			fmt.Sprintf("Expected *TerrakubeConnectionData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	if providerData.InsecureHttpClient {
		if custom, ok := http.DefaultTransport.(*http.Transport); ok {
			customTransport := custom.Clone()
			customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
			r.client = &http.Client{Transport: customTransport}
		} else {
			r.client = &http.Client{}
		}
	} else {
		r.client = &http.Client{}
	}

	r.endpoint = providerData.Endpoint
	r.token = providerData.Token

	tflog.Debug(ctx, "Configuring Collection Item resource", map[string]any{"success": true})
}

func (r *CollectionItemResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan CollectionItemResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.CollectionItemEntity{
		Key:         plan.Key.ValueString(),
		Value:       plan.Value.ValueString(),
		Description: plan.Description.ValueString(),
		Sensitive:   plan.Sensitive.ValueBool(),
		Category:    plan.Category.ValueString(),
		Hcl:         plan.Hcl.ValueBool(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	collectionItemRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/organization/%s/collection/%s/item", r.endpoint, plan.OrganizationId.ValueString(), plan.CollectionId.ValueString()), strings.NewReader(out.String()))
	collectionItemRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	collectionItemRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating collection item resource request", fmt.Sprintf("Error creating collection item resource request: %s", err))
		return
	}

	collectionItemResponse, err := r.client.Do(collectionItemRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing collection item resource request", fmt.Sprintf("Error executing collection item resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(collectionItemResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading collection item resource response")
	}
	collectionItem := &client.CollectionItemEntity{}

	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), collectionItem)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	if collectionItem.Sensitive {
		tflog.Info(ctx, "Collection item value is not included in response, setting values the same as the plan for sensitive=true...")
		plan.Value = types.StringValue(plan.Value.ValueString())
	} else {
		tflog.Info(ctx, "Collection item is included in response...")
		plan.Value = types.StringValue(collectionItem.Value)
	}

	plan.Key = types.StringValue(collectionItem.Key)
	plan.Description = types.StringValue(collectionItem.Description)
	plan.Category = types.StringValue(collectionItem.Category)
	plan.Sensitive = types.BoolValue(collectionItem.Sensitive)
	plan.Hcl = types.BoolValue(collectionItem.Hcl)
	plan.ID = types.StringValue(collectionItem.ID)

	tflog.Info(ctx, "collection item Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *CollectionItemResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state CollectionItemResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	collectionItemRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/collection/%s/item/%s", r.endpoint, state.OrganizationId.ValueString(), state.CollectionId.ValueString(), state.ID.ValueString()), nil)
	collectionItemRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	collectionItemRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating collection resource request", fmt.Sprintf("Error creating collection item resource request: %s", err))
		return
	}

	collectionItemResponse, err := r.client.Do(collectionItemRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing collection item resource request", fmt.Sprintf("Error executing collection item resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(collectionItemResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading collection item resource response")
	}
	collectionItem := &client.CollectionItemEntity{}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), collectionItem)

	if err != nil {
		//Response is not what we expected, perhaps it is a detailed error message, so more detail wanted
		ErrorsItem := &client.ErrorsEntity{}
		err2 := json.Unmarshal([]byte(string(bodyResponse)), ErrorsItem)
		if err2 != nil {
			//No, not a detailed error either
			resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s %s", err, err2))
			tflog.Debug(ctx, fmt.Sprintf("Data returned not as expected: %s", string(bodyResponse)))
			return
		} else if len(ErrorsItem.Errors) > 0 && regexp.MustCompile(`Unknown identifier .* for item`).MatchString(ErrorsItem.Errors[0].DetailedError) {
			//this might be a known issue where the variable is removed in the gui and added back manually with the same key but a different id
			tflog.Debug(ctx, "this might be a known issue where the variable is removed in the gui and added back manually with the same key but a different id")
			//lets query on key to see if we can find it
			collectionItemRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/collection/%s/item?filter[item]=key==%s", r.endpoint, state.OrganizationId.ValueString(), state.CollectionId.ValueString(), state.Key.ValueString()), nil)
			collectionItemRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
			collectionItemRequest.Header.Add("Content-Type", "application/vnd.api+json")
			if err != nil {
				resp.Diagnostics.AddError("Error creating collection resource request", fmt.Sprintf("Error creating collection item resource request: %s", err))
				return
			}

			collectionItemResponse, err := r.client.Do(collectionItemRequest)
			if err != nil {
				resp.Diagnostics.AddError("Error executing collection item resource request", fmt.Sprintf("Error executing collection item resource request: %s", err))
				return
			}

			bodyResponse, err := io.ReadAll(collectionItemResponse.Body)
			if err != nil {
				tflog.Error(ctx, "Error reading collection item resource response")
			}
			collectionItem = &client.CollectionItemEntity{}

			//try to unmarshal but removing the [ ] from around the response, because we want only one match
			err = jsonapi.UnmarshalPayload(strings.NewReader(strings.Replace(strings.Replace(string(bodyResponse), "]", "", 1), "[", "", 1)), collectionItem)

			if err != nil {
				resp.Diagnostics.AddError("Error unmarshal second payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
			} else {
				tflog.Info(ctx, fmt.Sprintf("Successfully found the new id for this variable %s => %s", state.ID.ValueString(), collectionItem.ID))
			}
		} else {
			tflog.Debug(ctx, "Detailed response contains an error")
			for _, e := range ErrorsItem.Errors {
				resp.Diagnostics.AddError("Error reading values from variable : ", fmt.Sprint(e.DetailedError))
			}
			return
		}
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	if collectionItem.Sensitive {
		tflog.Info(ctx, "Collection item value is not included in response, setting values the same as the current state value")
		state.Value = types.StringValue(state.Value.ValueString())
	} else {
		tflog.Info(ctx, "Collection item value is included in response...")
		state.Value = types.StringValue(collectionItem.Value)
	}

	state.Key = types.StringValue(collectionItem.Key)
	state.Description = types.StringValue(collectionItem.Description)
	state.Category = types.StringValue(collectionItem.Category)
	state.Sensitive = types.BoolValue(collectionItem.Sensitive)
	state.Hcl = types.BoolValue(collectionItem.Hcl)
	state.ID = types.StringValue(collectionItem.ID)

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Collection item Resource reading", map[string]any{"success": true})
}

func (r *CollectionItemResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan CollectionItemResourceModel
	var state CollectionItemResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.CollectionItemEntity{
		Key:         plan.Key.ValueString(),
		Value:       plan.Value.ValueString(),
		Description: plan.Description.ValueString(),
		Category:    plan.Category.ValueString(),
		Sensitive:   plan.Sensitive.ValueBool(),
		Hcl:         plan.Hcl.ValueBool(),
		ID:          state.ID.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	collectionItemReq, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/collection/%s/item/%s", r.endpoint, state.OrganizationId.ValueString(), state.CollectionId.ValueString(), state.ID.ValueString()), strings.NewReader(out.String()))
	collectionItemReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	collectionItemReq.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating collection item resource request", fmt.Sprintf("Error creating collection item resource request: %s", err))
		return
	}

	collectionItemResponse, err := r.client.Do(collectionItemReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing collection item resource request", fmt.Sprintf("Error executing collection item resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(collectionItemResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading collection item resource response")
	}

	tflog.Info(ctx, "Body Response", map[string]any{"success": string(bodyResponse)})

	collectionItemReq, err = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/collection/%s/item/%s", r.endpoint, state.OrganizationId.ValueString(), state.CollectionId.ValueString(), state.ID.ValueString()), nil)
	collectionItemReq.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	collectionItemReq.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating collection item resource request", fmt.Sprintf("Error creating collection item resource request: %s", err))
		return
	}

	collectionItemResponse, err = r.client.Do(collectionItemReq)
	if err != nil {
		resp.Diagnostics.AddError("Error executing collection item resource request", fmt.Sprintf("Error executing collection item resource request: %s", err))
		return
	}

	bodyResponse, err = io.ReadAll(collectionItemResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading collection item resource response body", fmt.Sprintf("Error reading collection item resource response body: %s", err))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	collectionItem := &client.CollectionItemEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), collectionItem)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	if collectionItem.Sensitive {
		tflog.Info(ctx, "Collection item is not included in response, setting values the same as the plan for sensitive=true...")
		plan.Value = types.StringValue(plan.Value.ValueString())
	} else {
		tflog.Info(ctx, "Collection value is included in response...")
		plan.Value = types.StringValue(collectionItem.Value)
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.Key = types.StringValue(collectionItem.Key)
	plan.Description = types.StringValue(collectionItem.Description)
	plan.Category = types.StringValue(collectionItem.Category)
	plan.Sensitive = types.BoolValue(collectionItem.Sensitive)
	plan.Hcl = types.BoolValue(collectionItem.Hcl)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *CollectionItemResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data CollectionItemResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	workspaceRequest, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/organization/%s/collection/%s/item/%s", r.endpoint, data.OrganizationId.ValueString(), data.CollectionId.ValueString(), data.ID.ValueString()), nil)
	workspaceRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	if err != nil {
		resp.Diagnostics.AddError("Error creating collection item resource request", fmt.Sprintf("Error creating collection item resource request: %s", err))
		return
	}

	_, err = r.client.Do(workspaceRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing collection item resource request", fmt.Sprintf("Error executing collection item resource request: %s", err))
		return
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
