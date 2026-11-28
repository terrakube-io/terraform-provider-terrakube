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

var _ resource.Resource = &FederatedCredentialClaimResource{}
var _ resource.ResourceWithImportState = &FederatedCredentialClaimResource{}

type FederatedCredentialClaimResource struct {
	client *terrakube.Client
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
				Description: "Terrakube federated credential ID this claim condition belongs to. Changing this forces a new resource.",
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
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Federated Credential Claim Resource")
}

func (r *FederatedCredentialClaimResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan FederatedCredentialClaimResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.createString("claimKey", plan.ClaimKey)
	attributes.createString("claimValue", plan.ClaimValue)

	claim, err := r.client.FederatedClaims.CreateFields(ctx, plan.FederatedCredentialId.ValueString(), terrakube.Patch{Attributes: attributes})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating federated credential claim", err)
		return
	}

	plan.ID = types.StringValue(claim.ID)
	plan.ClaimKey = types.StringValue(claim.ClaimKey)
	plan.ClaimValue = types.StringValue(claim.ClaimValue)

	tflog.Info(ctx, "Federated Credential Claim Resource Created", map[string]any{"success": true})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *FederatedCredentialClaimResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state FederatedCredentialClaimResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	claim, err := r.client.FederatedClaims.Get(ctx, state.FederatedCredentialId.ValueString(), state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Federated credential claim not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading federated credential claim", err)
		return
	}

	state.ID = types.StringValue(claim.ID)
	state.ClaimKey = types.StringValue(claim.ClaimKey)
	state.ClaimValue = types.StringValue(claim.ClaimValue)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *FederatedCredentialClaimResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan FederatedCredentialClaimResourceModel
	var state FederatedCredentialClaimResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.updateString("claimKey", plan.ClaimKey, state.ClaimKey)
	attributes.updateString("claimValue", plan.ClaimValue, state.ClaimValue)

	federatedID, id := state.FederatedCredentialId.ValueString(), state.ID.ValueString()
	claim, err := r.client.FederatedClaims.UpdateFields(ctx, federatedID, id, terrakube.Patch{Attributes: attributes})
	if patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating federated credential claim", err)
		return
	}

	claim, err = readAfterPatch(claim, func() (*terrakube.FederatedClaim, error) {
		return r.client.FederatedClaims.Get(ctx, federatedID, id)
	})
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading federated credential claim after update", err)
		return
	}

	plan.ID = types.StringValue(id)
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

	err := r.client.FederatedClaims.Delete(ctx, data.FederatedCredentialId.ValueString(), data.ID.ValueString())
	reportDeleteError(&resp.Diagnostics, "deleting federated credential claim", err)
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
