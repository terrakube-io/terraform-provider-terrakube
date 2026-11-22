package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"terraform-provider-terrakube/internal/helpers"

	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	terrakube "github.com/terrakube-io/terrakube-go"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &VcsResource{}
var _ resource.ResourceWithImportState = &VcsResource{}
var _ resource.ResourceWithConfigValidators = &VcsResource{}

type VcsResource struct {
	client *terrakube.Client
}

type VcsResourceModel struct {
	ID                    types.String `tfsdk:"id"`
	OrganizationId        types.String `tfsdk:"organization_id"`
	Name                  types.String `tfsdk:"name"`
	Description           types.String `tfsdk:"description"`
	VcsType               types.String `tfsdk:"vcs_type"`
	ConnectionType        types.String `tfsdk:"connection_type"`
	ClientId              types.String `tfsdk:"client_id"`
	ClientSecret          types.String `tfsdk:"client_secret"`
	ClientSecretWo        types.String `tfsdk:"client_secret_wo"`
	ClientSecretWoVersion types.Int64  `tfsdk:"client_secret_wo_version"`
	PrivateKey            types.String `tfsdk:"private_key"`
	PrivateKeyWo          types.String `tfsdk:"private_key_wo"`
	PrivateKeyWoVersion   types.Int64  `tfsdk:"private_key_wo_version"`
	Endpoint              types.String `tfsdk:"endpoint"`
	ApiUrl                types.String `tfsdk:"api_url"`
	Status                types.String `tfsdk:"status"`
	ConnectUrl            types.String `tfsdk:"connect_url"`
}

func NewVcsResource() resource.Resource {
	return &VcsResource{}
}

func (r *VcsResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vcs"
}

func (r *VcsResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a VCS provider for the desired organization. VCS are used by VCS workspace to read " +
			"github repositories at run time.",

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
			"name": schema.StringAttribute{
				Required:    true,
				Description: "The name of the VCS connection",
			},
			"description": schema.StringAttribute{
				Required:    true,
				Description: "The description of the VCS connection",
			},
			"vcs_type": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("GITHUB"),
				Description: "Variable description",
				Validators: []validator.String{
					stringvalidator.OneOf("GITHUB", "GITLAB", "BITBUCKET", "AZURE_DEVOPS"),
				},
			},
			"connection_type": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("OAUTH"),
				Description: "The connection type of the VCS connection, valid vaules are `OAUTH` and `STANDALONE`, default is `OAUTH`. `STANDALONE` is used for GitHub App only.",
				Validators: []validator.String{
					stringvalidator.OneOf("OAUTH", "STANDALONE"),
				},
			},
			"client_id": schema.StringAttribute{
				Required:    true,
				Description: "The client ID or GitHub Application ID for the VCS connection",
			},
			"client_secret": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "The secret of the VCS connection, stored in the Terraform state. At least one of `client_secret`, `client_secret_wo`, `private_key` or `private_key_wo` is required. Changing this forces a new resource, unless it moves to `client_secret_wo`.",
				PlanModifiers: []planmodifier.String{
					requiresReplaceUnlessMovedToWriteOnly("client_secret"),
				},
			},
			"client_secret_wo":         writeOnlyAttribute("client_secret", "The secret of the VCS connection", false),
			"client_secret_wo_version": writeOnlyVersionAttribute("client_secret"),
			"private_key": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "The private key in PKCS8 format of the VCS connection. Please use command `openssl pkcs8 -topk8 -inform PEM -inform pem -outform pem -in github_rsa_private_key.pem -out private_key.pem -nocrypt` to convert the private key to PKCS8 format form Github default RSA. Changing this forces a new resource, unless it moves to `private_key_wo`.",
				PlanModifiers: []planmodifier.String{
					requiresReplaceUnlessMovedToWriteOnly("private_key"),
				},
			},
			"private_key_wo":         writeOnlyAttribute("private_key", "The private key in PKCS8 format of the VCS connection", false),
			"private_key_wo_version": writeOnlyVersionAttribute("private_key"),
			"endpoint": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					stringvalidator.AlsoRequires(path.MatchRelative().AtParent().AtName("api_url")),
					stringvalidator.RegexMatches(regexp.MustCompile(`^https?://.*$`), "The endpoint must be a valid URL"),
				},
				Description: "The endpoint of the VCS connection",
			},
			"api_url": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "The API URL of the VCS connection",
				Validators: []validator.String{
					stringvalidator.RegexMatches(regexp.MustCompile(`^https?://.*$`), "The endpoint must be a valid URL"),
				},
			},
			"connect_url": schema.StringAttribute{
				Computed:    true,
				Description: "The connect URL of the VCS connection, after adding the VCS connection, please logon to this URL to connect.",
			},
			"status": schema.StringAttribute{
				Computed: true,
				Default:  stringdefault.StaticString("PENDING"),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Description: "The status of the VCS connection. IMPORTANT NOTE: if the status is not 'PENDING', please logon to the connect_url to connect!!.",
			},
		},
	}
}

// requiresReplaceUnlessMovedToWriteOnly replaces the connection when attr
// changes, except when it is removed in favour of attr_wo: moving a secret to
// its write-only argument sends the same value with an update.
func requiresReplaceUnlessMovedToWriteOnly(attr string) planmodifier.String {
	return stringplanmodifier.RequiresReplaceIf(
		func(ctx context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
			if !req.ConfigValue.IsNull() {
				resp.RequiresReplace = true
				return
			}

			var valueWo types.String
			resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(attr+"_wo"), &valueWo)...)
			resp.RequiresReplace = valueWo.IsNull()
		},
		fmt.Sprintf("Changing %s replaces the VCS connection, unless it moves to %s_wo.", attr, attr),
		fmt.Sprintf("Changing `%s` replaces the VCS connection, unless it moves to `%s_wo`.", attr, attr),
	)
}

func (r *VcsResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.AtLeastOneOf(
			path.MatchRoot("client_secret"),
			path.MatchRoot("client_secret_wo"),
			path.MatchRoot("private_key"),
			path.MatchRoot("private_key_wo"),
		),
		resourcevalidator.Conflicting(
			path.MatchRoot("client_secret"),
			path.MatchRoot("client_secret_wo"),
		),
		resourcevalidator.Conflicting(
			path.MatchRoot("private_key"),
			path.MatchRoot("private_key_wo"),
		),
	}
}

// vcsSecrets returns client_secret and private_key to send to Terrakube, from
// the plan or from their write-only arguments in the configuration.
func vcsSecrets(ctx context.Context, config tfsdk.Config, plan VcsResourceModel) (string, string, diag.Diagnostics) {
	clientSecret, _, diags := writeOnlyValue(ctx, config, plan.ClientSecret, "client_secret")
	privateKey, _, keyDiags := writeOnlyValue(ctx, config, plan.PrivateKey, "private_key")
	diags.Append(keyDiags...)
	return clientSecret, privateKey, diags
}

func (r *VcsResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "VCS Resource")
}

func (r *VcsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan VcsResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	clientSecret, privateKey, diags := vcsSecrets(ctx, req.Config, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Both secrets are always sent, empty when not set, as Terrakube has always received them.
	attributes := patchAttributes{"clientSecret": clientSecret, "privateKey": privateKey}
	attributes.createString("name", plan.Name)
	attributes.createString("description", plan.Description)
	attributes.createString("vcsType", plan.VcsType)
	attributes.createString("connectionType", plan.ConnectionType)
	attributes.createString("clientId", plan.ClientId)
	attributes.createString("endpoint", plan.Endpoint)
	attributes.createString("apiUrl", plan.ApiUrl)
	attributes.createString("status", plan.Status)

	vcs, err := r.client.VCS.CreateFields(ctx, plan.OrganizationId.ValueString(), terrakube.Patch{Attributes: attributes})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating vcs connection", err)
		return
	}

	plan.ID = types.StringValue(vcs.ID)
	plan.Name = types.StringValue(vcs.Name)
	plan.Description = types.StringValue(vcs.Description)
	plan.VcsType = types.StringValue(vcs.VcsType)
	plan.ClientId = types.StringValue(vcs.ClientID)
	plan.Endpoint = types.StringValue(vcs.Endpoint)
	plan.ApiUrl = types.StringValue(vcs.APIURL)
	// client_secret and private_key stay as planned (null with their _wo arguments):
	// Terrakube does not return them.
	plan.ClientSecretWo = types.StringNull()
	plan.PrivateKeyWo = types.StringNull()
	plan.ConnectUrl = types.StringValue(plan.ConnectUrl.ValueString())
	plan.Status = types.StringValue(vcs.Status)
	plan.ConnectionType = types.StringValue(vcs.ConnectionType)

	if vcs.Status == "PENDING" {
		tflog.Warn(ctx, fmt.Sprintf("VCS connection is pending, please logon to %s to connect. Check doc here %s", plan.ConnectUrl, helpers.GetVCSProviderDoc()))
	}
	tflog.Info(ctx, "VCS Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *VcsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state VcsResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vcs, err := r.client.VCS.Get(ctx, state.OrganizationId.ValueString(), state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "VCS not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading vcs connection", err)
		return
	}

	// client_secret and private_key are kept from the state: Terrakube does not return them.
	state.ID = types.StringValue(vcs.ID)
	state.Name = types.StringValue(vcs.Name)
	state.Description = types.StringValue(vcs.Description)
	state.VcsType = types.StringValue(vcs.VcsType)
	state.ConnectionType = types.StringValue(vcs.ConnectionType)
	state.ClientId = types.StringValue(vcs.ClientID)
	state.Endpoint = types.StringValue(vcs.Endpoint)
	state.ApiUrl = types.StringValue(vcs.APIURL)
	state.Status = types.StringValue(vcs.Status)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if vcs.Status == "PENDING" {
		tflog.Warn(ctx, fmt.Sprintf("VCS connection is pending, please logon to %s to connect. Check doc here %s", state.ConnectUrl, helpers.GetVCSProviderDoc()))
	}
}

func (r *VcsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan VcsResourceModel
	var state VcsResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	clientSecret, privateKey, diags := vcsSecrets(ctx, req.Config, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Both secrets are always sent: a write-only secret is null in plan and state, so there is no
	// prior value to compare with.
	attributes := patchAttributes{"clientSecret": clientSecret, "privateKey": privateKey}
	attributes.updateString("name", plan.Name, state.Name)
	attributes.updateString("description", plan.Description, state.Description)
	attributes.updateString("vcsType", plan.VcsType, state.VcsType)
	attributes.updateString("connectionType", plan.ConnectionType, state.ConnectionType)
	attributes.updateString("clientId", plan.ClientId, state.ClientId)
	attributes.updateString("endpoint", plan.Endpoint, state.Endpoint)
	attributes.updateString("apiUrl", plan.ApiUrl, state.ApiUrl)
	attributes.updateString("status", plan.Status, state.Status)

	organizationID, id := state.OrganizationId.ValueString(), state.ID.ValueString()
	if _, err := r.client.VCS.UpdateFields(ctx, organizationID, id, terrakube.Patch{Attributes: attributes}); patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating vcs connection", err)
		return
	}

	vcs, err := r.client.VCS.Get(ctx, organizationID, id)
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading vcs connection after update", err)
		return
	}

	plan.ID = types.StringValue(id)
	plan.Name = types.StringValue(vcs.Name)
	plan.Description = types.StringValue(vcs.Description)
	plan.ConnectionType = types.StringValue(vcs.ConnectionType)
	plan.VcsType = types.StringValue(vcs.VcsType)
	plan.ClientId = types.StringValue(vcs.ClientID)
	// client_secret and private_key stay as planned: Terrakube does not return them.
	plan.Endpoint = types.StringValue(vcs.Endpoint)
	plan.ApiUrl = types.StringValue(vcs.APIURL)
	plan.Status = types.StringValue(vcs.Status)
	plan.ConnectUrl = types.StringValue(plan.ConnectUrl.ValueString())
	plan.ClientSecretWo = types.StringNull()
	plan.PrivateKeyWo = types.StringNull()

	if vcs.Status == "PENDING" {
		tflog.Warn(ctx, fmt.Sprintf("VCS connection is pending, please logon to %s to connect. Check doc here %s", plan.ConnectUrl, helpers.GetVCSProviderDoc()))
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *VcsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data VcsResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.VCS.Delete(ctx, data.OrganizationId.ValueString(), data.ID.ValueString())
	reportDeleteError(&resp.Diagnostics, "deleting vcs connection", err)
}

func (r *VcsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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

func GetEndpointAndApiUrl(vcs_type string, clientId string, supplied_endpoint string) (string, string, string) {
	var endpoint, api_url, connect_url string
	switch vcs_type {
	case "GITHUB":
		if supplied_endpoint != "" {
			endpoint = supplied_endpoint
		} else {
			endpoint = "https://github.com"
		}
		connect_url = fmt.Sprintf("%s/login/oauth/authorize?client_id=%s&allow_signup=false&scope=repo", endpoint, clientId)
		api_url = "https://api.github.com"
	case "GITLAB":
		if supplied_endpoint != "" {
			endpoint = supplied_endpoint
		} else {
			endpoint = "https://gitlab.com"
		}
		connect_url = fmt.Sprintf("%s/oauth/authorize?client_id=%s&response_type=code&scope=api", endpoint, clientId)
		api_url = "https://gitlab.com/api/v4"
	case "BITBUCKET":
		if supplied_endpoint != "" {
			endpoint = supplied_endpoint
		} else {
			endpoint = "https://bitbucket.org"
		}
		connect_url = fmt.Sprintf("%s/site/oauth2/authorize?client_id=%s&response_type=code&response_type=code&scope=repository", endpoint, clientId)
		api_url = "https://api.bitbucket.org/2.0"
	case "AZURE_DEVOPS":
		if supplied_endpoint != "" {
			endpoint = supplied_endpoint
		} else {
			endpoint = "https://dev.azure.com"
		}
		connect_url = fmt.Sprintf("%s/oauth2/authorize?client_id=%s&response_type=Assertion&scope=vso.code+vso.code_status", endpoint, clientId)
		api_url = "https://dev.azure.com"
	}
	return endpoint, api_url, connect_url
}

func (r VcsResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// Do nothing if it's destroy
	if req.Plan.Raw.IsNull() {
		return
	}
	var plan VcsResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	// If it's not a create operation, we don't need to update the status
	if !req.State.Raw.IsNull() {
		var state VcsResourceModel
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		plan.Status = types.StringValue(state.Status.ValueString())
	}

	if resp.Diagnostics.HasError() {
		return
	}

	endpoint, apiUrl, connectUrl := GetEndpointAndApiUrl(plan.VcsType.ValueString(), plan.ClientId.ValueString(), plan.Endpoint.ValueString())
	if plan.Endpoint.ValueString() == "" {
		plan.Endpoint = types.StringValue(endpoint)
	}
	if plan.ApiUrl.ValueString() == "" {
		plan.ApiUrl = types.StringValue(apiUrl)
	}
	if plan.ConnectionType.Equal(types.StringValue("STANDALONE")) {
		plan.Status = types.StringValue("COMPLETED")
	}
	plan.ConnectUrl = types.StringValue(connectUrl)

	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}
