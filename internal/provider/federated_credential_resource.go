package provider

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"terraform-provider-terrakube/internal/client"

	"github.com/google/jsonapi"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ resource.Resource = &FederatedCredentialResource{}
var _ resource.ResourceWithImportState = &FederatedCredentialResource{}

type FederatedCredentialResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type FederatedCredentialResourceModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	IssuerUrl types.String `tfsdk:"issuer_url"`
	Audience  types.String `tfsdk:"audience"`
}

func NewFederatedCredentialResource() resource.Resource {
	return &FederatedCredentialResource{}
}

func (r *FederatedCredentialResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_federated_credential"
}

func (r *FederatedCredentialResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a federated credential. Federated credentials establish a trust relationship between Terrakube and an external identity provider (such as GitHub Actions, GitLab CI or Azure AD), allowing tokens issued by that provider to authenticate against Terrakube.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Federated credential ID",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Federated credential name. Example: \"GitHub Actions\"",
			},
			"issuer_url": schema.StringAttribute{
				Required:    true,
				Description: "Issuer URL of the external identity provider. Example: \"https://token.actions.githubusercontent.com\"",
			},
			"audience": schema.StringAttribute{
				Required:    true,
				Description: "Audience expected in tokens issued by the identity provider. Example: \"terrakube-audience\"",
			},
		},
	}
}

func (r *FederatedCredentialResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Federated Credential Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Federated Credential resource", map[string]any{"success": true})
}

func (r *FederatedCredentialResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan FederatedCredentialResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.FederatedEntity{
		Name:      plan.Name.ValueString(),
		IssuerUrl: plan.IssuerUrl.ValueString(),
		Audience:  plan.Audience.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)
	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	federatedRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/federated", r.endpoint), strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating federated credential resource request", fmt.Sprintf("Error creating federated credential resource request: %s", err))
		return
	}
	federatedRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	federatedRequest.Header.Add("Content-Type", "application/vnd.api+json")

	federatedResponse, err := r.client.Do(federatedRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing federated credential resource request", fmt.Sprintf("Error executing federated credential resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(federatedResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading federated credential resource response")
	}

	newFederated := &client.FederatedEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), newFederated)
	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	plan.ID = types.StringValue(newFederated.ID)
	plan.Name = types.StringValue(newFederated.Name)
	plan.IssuerUrl = types.StringValue(newFederated.IssuerUrl)
	plan.Audience = types.StringValue(newFederated.Audience)

	tflog.Info(ctx, "Federated Credential Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *FederatedCredentialResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state FederatedCredentialResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	federatedRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/federated/%s", r.endpoint, state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating federated credential resource request", fmt.Sprintf("Error creating federated credential resource request: %s", err))
		return
	}
	federatedRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	federatedRequest.Header.Add("Content-Type", "application/vnd.api+json")

	federatedResponse, err := r.client.Do(federatedRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing federated credential resource request", fmt.Sprintf("Error executing federated credential resource request: %s", err))
		return
	}

	if federatedResponse.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Federated credential not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	bodyResponse, err := io.ReadAll(federatedResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading federated credential resource response")
	}

	federated := &client.FederatedEntity{}
	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), federated)
	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	state.ID = types.StringValue(federated.ID)
	state.Name = types.StringValue(federated.Name)
	state.IssuerUrl = types.StringValue(federated.IssuerUrl)
	state.Audience = types.StringValue(federated.Audience)

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Federated Credential Resource reading", map[string]any{"success": true})
}

func (r *FederatedCredentialResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan FederatedCredentialResourceModel
	var state FederatedCredentialResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.FederatedEntity{
		ID:        state.ID.ValueString(),
		Name:      plan.Name.ValueString(),
		IssuerUrl: plan.IssuerUrl.ValueString(),
		Audience:  plan.Audience.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)
	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	federatedRequest, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/federated/%s", r.endpoint, state.ID.ValueString()), strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating federated credential resource request", fmt.Sprintf("Error creating federated credential resource request: %s", err))
		return
	}
	federatedRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	federatedRequest.Header.Add("Content-Type", "application/vnd.api+json")

	federatedResponse, err := r.client.Do(federatedRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing federated credential resource request", fmt.Sprintf("Error executing federated credential resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(federatedResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading federated credential resource response")
	}
	tflog.Info(ctx, "Body Response", map[string]any{"success": string(bodyResponse)})

	federatedRequest, err = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/federated/%s", r.endpoint, state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating federated credential resource request", fmt.Sprintf("Error creating federated credential resource request: %s", err))
		return
	}
	federatedRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	federatedRequest.Header.Add("Content-Type", "application/vnd.api+json")

	federatedResponse, err = r.client.Do(federatedRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing federated credential resource request", fmt.Sprintf("Error executing federated credential resource request: %s", err))
		return
	}

	bodyResponse, err = io.ReadAll(federatedResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading federated credential resource response body", fmt.Sprintf("Error reading federated credential resource response body: %s", err))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	federated := &client.FederatedEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), federated)
	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.Name = types.StringValue(federated.Name)
	plan.IssuerUrl = types.StringValue(federated.IssuerUrl)
	plan.Audience = types.StringValue(federated.Audience)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *FederatedCredentialResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data FederatedCredentialResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	federatedRequest, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/federated/%s", r.endpoint, data.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating federated credential resource request", fmt.Sprintf("Error creating federated credential resource request: %s", err))
		return
	}
	federatedRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))

	_, err = r.client.Do(federatedRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing federated credential resource request", fmt.Sprintf("Error executing federated credential resource request: %s", err))
		return
	}
}

func (r *FederatedCredentialResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
