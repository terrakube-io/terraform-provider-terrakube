package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	terrakube "github.com/terrakube-io/terrakube-go"
)

var _ resource.Resource = &FederatedCredentialResource{}
var _ resource.ResourceWithImportState = &FederatedCredentialResource{}

type FederatedCredentialResource struct {
	client *terrakube.Client
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
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Federated Credential Resource")
}

func (r *FederatedCredentialResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan FederatedCredentialResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.createString("name", plan.Name)
	attributes.createString("issuerUrl", plan.IssuerUrl)
	attributes.createString("audience", plan.Audience)

	federated, err := r.client.Federated.CreateFields(ctx, terrakube.Patch{Attributes: attributes})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating federated credential", err)
		return
	}

	plan.ID = types.StringValue(federated.ID)
	plan.Name = types.StringValue(federated.Name)
	plan.IssuerUrl = types.StringValue(federated.IssuerURL)
	plan.Audience = types.StringValue(federated.Audience)

	tflog.Info(ctx, "Federated Credential Resource Created", map[string]any{"success": true})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *FederatedCredentialResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state FederatedCredentialResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	federated, err := r.client.Federated.Get(ctx, state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "Federated credential not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading federated credential", err)
		return
	}

	state.ID = types.StringValue(federated.ID)
	state.Name = types.StringValue(federated.Name)
	state.IssuerUrl = types.StringValue(federated.IssuerURL)
	state.Audience = types.StringValue(federated.Audience)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *FederatedCredentialResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan FederatedCredentialResourceModel
	var state FederatedCredentialResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{}
	attributes.updateString("name", plan.Name, state.Name)
	attributes.updateString("issuerUrl", plan.IssuerUrl, state.IssuerUrl)
	attributes.updateString("audience", plan.Audience, state.Audience)

	id := state.ID.ValueString()
	if _, err := r.client.Federated.UpdateFields(ctx, id, terrakube.Patch{Attributes: attributes}); patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating federated credential", err)
		return
	}

	federated, err := r.client.Federated.Get(ctx, id)
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading federated credential after update", err)
		return
	}

	plan.ID = types.StringValue(id)
	plan.Name = types.StringValue(federated.Name)
	plan.IssuerUrl = types.StringValue(federated.IssuerURL)
	plan.Audience = types.StringValue(federated.Audience)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *FederatedCredentialResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data FederatedCredentialResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Federated.Delete(ctx, data.ID.ValueString())
	reportDeleteError(&resp.Diagnostics, "deleting federated credential", err)
}

func (r *FederatedCredentialResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
