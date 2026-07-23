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

var _ resource.Resource = &FederatedCredentialClaimResource{}
var _ resource.ResourceWithImportState = &FederatedCredentialClaimResource{}

type FederatedCredentialClaimResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type FederatedCredentialClaimResourceModel struct {
	ID                    types.String `tfsdk:"id"`
	FederatedCredentialId types.String `tfsdk:"federated_credential_id"`
	ClaimKey              types.String `tfsdk:"claim_key"`
	ClaimValue            types.String `tfsdk:"claim_value"`
}

func NewFederatedCredentialClaimResource() resource.Resource {
	return &FederatedCredentialClaimResource{}
}

func (r *FederatedCredentialClaimResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_federated_credential_claim"
}

func (r *FederatedCredentialClaimResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a claim condition for a federated credential. Claim conditions restrict which tokens are accepted from the identity provider: every condition must match for a token to be authorized. Examples: `repository_owner` (GitHub Actions), `groups_direct` (GitLab CI), `amr` (Azure AD).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Federated credential claim ID",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"federated_credential_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube federated credential ID this claim condition belongs to",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"claim_key": schema.StringAttribute{
				Required:    true,
				Description: "Claim key to match in the identity provider token. Example: \"repository_owner\"",
			},
			"claim_value": schema.StringAttribute{
				Required:    true,
				Description: "Expected value for the claim key. Example: \"terrakube-org\"",
			},
		},
	}
}

func (r *FederatedCredentialClaimResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Federated Credential Claim Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Federated Credential Claim resource", map[string]any{"success": true})
}

func (r *FederatedCredentialClaimResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan FederatedCredentialClaimResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.FederatedClaimEntity{
		ClaimKey:   plan.ClaimKey.ValueString(),
		ClaimValue: plan.ClaimValue.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)
	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	claimRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/federated/%s/claims", r.endpoint, plan.FederatedCredentialId.ValueString()), strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating federated credential claim resource request", fmt.Sprintf("Error creating federated credential claim resource request: %s", err))
		return
	}
	claimRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	claimRequest.Header.Add("Content-Type", "application/vnd.api+json")

	claimResponse, err := r.client.Do(claimRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing federated credential claim resource request", fmt.Sprintf("Error executing federated credential claim resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(claimResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading federated credential claim resource response")
	}

	newClaim := &client.FederatedClaimEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), newClaim)
	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	plan.ID = types.StringValue(newClaim.ID)
	plan.ClaimKey = types.StringValue(newClaim.ClaimKey)
	plan.ClaimValue = types.StringValue(newClaim.ClaimValue)

	tflog.Info(ctx, "Federated Credential Claim Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *FederatedCredentialClaimResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state FederatedCredentialClaimResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	claimRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/federated/%s/claims/%s", r.endpoint, state.FederatedCredentialId.ValueString(), state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating federated credential claim resource request", fmt.Sprintf("Error creating federated credential claim resource request: %s", err))
		return
	}
	claimRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	claimRequest.Header.Add("Content-Type", "application/vnd.api+json")

	claimResponse, err := r.client.Do(claimRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing federated credential claim resource request", fmt.Sprintf("Error executing federated credential claim resource request: %s", err))
		return
	}

	if claimResponse.StatusCode == http.StatusNotFound {
		tflog.Warn(ctx, "Federated credential claim not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	bodyResponse, err := io.ReadAll(claimResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading federated credential claim resource response")
	}

	claim := &client.FederatedClaimEntity{}
	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), claim)
	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	state.ID = types.StringValue(claim.ID)
	state.ClaimKey = types.StringValue(claim.ClaimKey)
	state.ClaimValue = types.StringValue(claim.ClaimValue)

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Federated Credential Claim Resource reading", map[string]any{"success": true})
}

func (r *FederatedCredentialClaimResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan FederatedCredentialClaimResourceModel
	var state FederatedCredentialClaimResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.FederatedClaimEntity{
		ID:         state.ID.ValueString(),
		ClaimKey:   plan.ClaimKey.ValueString(),
		ClaimValue: plan.ClaimValue.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)
	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	claimRequest, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/federated/%s/claims/%s", r.endpoint, state.FederatedCredentialId.ValueString(), state.ID.ValueString()), strings.NewReader(out.String()))
	if err != nil {
		resp.Diagnostics.AddError("Error creating federated credential claim resource request", fmt.Sprintf("Error creating federated credential claim resource request: %s", err))
		return
	}
	claimRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	claimRequest.Header.Add("Content-Type", "application/vnd.api+json")

	claimResponse, err := r.client.Do(claimRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing federated credential claim resource request", fmt.Sprintf("Error executing federated credential claim resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(claimResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading federated credential claim resource response")
	}
	tflog.Info(ctx, "Body Response", map[string]any{"success": string(bodyResponse)})

	claimRequest, err = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/federated/%s/claims/%s", r.endpoint, state.FederatedCredentialId.ValueString(), state.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating federated credential claim resource request", fmt.Sprintf("Error creating federated credential claim resource request: %s", err))
		return
	}
	claimRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	claimRequest.Header.Add("Content-Type", "application/vnd.api+json")

	claimResponse, err = r.client.Do(claimRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing federated credential claim resource request", fmt.Sprintf("Error executing federated credential claim resource request: %s", err))
		return
	}

	bodyResponse, err = io.ReadAll(claimResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading federated credential claim resource response body", fmt.Sprintf("Error reading federated credential claim resource response body: %s", err))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	claim := &client.FederatedClaimEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), claim)
	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.ClaimKey = types.StringValue(claim.ClaimKey)
	plan.ClaimValue = types.StringValue(claim.ClaimValue)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *FederatedCredentialClaimResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data FederatedCredentialClaimResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	claimRequest, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/federated/%s/claims/%s", r.endpoint, data.FederatedCredentialId.ValueString(), data.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating federated credential claim resource request", fmt.Sprintf("Error creating federated credential claim resource request: %s", err))
		return
	}
	claimRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))

	_, err = r.client.Do(claimRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing federated credential claim resource request", fmt.Sprintf("Error executing federated credential claim resource request: %s", err))
		return
	}
}

func (r *FederatedCredentialClaimResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) != 2 || idParts[0] == "" || idParts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: 'federated_credential_ID,ID', Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("federated_credential_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[1])...)
}
