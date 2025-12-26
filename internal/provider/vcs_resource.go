package provider

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"terraform-provider-terrakube/internal/client"
	"terraform-provider-terrakube/internal/helpers"

	"github.com/google/jsonapi"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &VcsResource{}
var _ resource.ResourceWithImportState = &VcsResource{}

type VcsResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type VcsResourceModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	VcsType        types.String `tfsdk:"vcs_type"`
	ConnectionType types.String `tfsdk:"connection_type"`
	ClientId       types.String `tfsdk:"client_id"`
	ClientSecret   types.String `tfsdk:"client_secret"`
	PrivateKey     types.String `tfsdk:"private_key"`
	Endpoint       types.String `tfsdk:"endpoint"`
	ApiUrl         types.String `tfsdk:"api_url"`
	Status         types.String `tfsdk:"status"`
	ConnectUrl     types.String `tfsdk:"connect_url"`
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
				Optional:    true,
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
				Description: "The secret of the VCS connection",
				Validators: []validator.String{
					stringvalidator.AtLeastOneOf(path.MatchRelative().AtParent().AtName("client_secret"), path.MatchRelative().AtParent().AtName("private_key")),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"private_key": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "The private key in PKCS8 format of the VCS connection. Please use command `openssl pkcs8 -topk8 -inform PEM -inform pem -outform pem -in github_rsa_private_key.pem -out private_key.pem -nocrypt` to convert the private key to PKCS8 format form Github default RSA.",
				Validators: []validator.String{
					stringvalidator.AtLeastOneOf(path.MatchRelative().AtParent().AtName("client_secret"), path.MatchRelative().AtParent().AtName("private_key")),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
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

func (r *VcsResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Organization Variable Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Organization Variable resource", map[string]any{"success": true})
}

func (r *VcsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan VcsResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.VcsEntity{
		Name:           plan.Name.ValueString(),
		Description:    plan.Description.ValueStringPointer(),
		VcsType:        plan.VcsType.ValueString(),
		ConnectionType: plan.ConnectionType.ValueString(),
		ClientId:       plan.ClientId.ValueString(),
		ClientSecret:   plan.ClientSecret.ValueString(),
		PrivateKey:     plan.PrivateKey.ValueString(),
		Endpoint:       plan.Endpoint.ValueString(),
		ApiUrl:         plan.ApiUrl.ValueString(),
		Status:         plan.Status.ValueString(),
	}
	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	vcsRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/organization/%s/vcs", r.endpoint, plan.OrganizationId.ValueString()), strings.NewReader(out.String()))
	vcsRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	vcsRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating VCS resource request", fmt.Sprintf("Error creating VCS resource request: %s", err))
		return
	}

	vcsResponse, err := r.client.Do(vcsRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing VCS resource request", fmt.Sprintf("Error executing VCS resource request, response status %s, response body %s, error: %s", vcsResponse.Status, vcsResponse.Request.Body, err))
		return
	}

	bodyResponse, err := io.ReadAll(vcsResponse.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading VCS resource response, error: %s, response status: %s", err, vcsResponse.Status))
	}
	vcs := &client.VcsEntity{}

	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), vcs)
	tflog.Info(ctx, string(bodyResponse))
	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response, error: %s, response status: %s", err, vcsResponse.Status))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	plan.ID = types.StringValue(vcs.ID)
	plan.Name = types.StringValue(vcs.Name)
	plan.Description = types.StringPointerValue(vcs.Description)
	plan.VcsType = types.StringValue(vcs.VcsType)
	plan.ClientId = types.StringValue(vcs.ClientId)
	plan.Endpoint = types.StringValue(vcs.Endpoint)
	plan.ApiUrl = types.StringValue(vcs.ApiUrl)
	tflog.Info(ctx, "Client secret and private key are not available in the response, setting to original value set in the request.")
	if plan.ClientSecret.ValueString() != "" {
		plan.ClientSecret = types.StringValue(plan.ClientSecret.ValueString())
	}
	if plan.PrivateKey.ValueString() != "" {
		plan.PrivateKey = types.StringValue(plan.PrivateKey.ValueString())
	}
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
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	vcsRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/vcs/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	vcsRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	vcsRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating VCS resource request", fmt.Sprintf("Error creating VCS resource request: %s", err))
		return
	}

	vcsResponse, err := r.client.Do(vcsRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing VCS resource request", fmt.Sprintf("Error executing VCS resource request, response status %s, response body %s, error: %s", vcsResponse.Status, vcsResponse.Request.Body, err))
		return
	}

	bodyResponse, err := io.ReadAll(vcsResponse.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading organization variable resource response, error: %s, response status: %s", err, vcsResponse.Status))
	}
	vcs := &client.VcsEntity{}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), vcs)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response, error: %s, response status: %s", err, vcsResponse.Status))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	state.ID = types.StringValue(vcs.ID)
	state.Name = types.StringValue(vcs.Name)
	state.Description = types.StringPointerValue(vcs.Description)
	state.VcsType = types.StringValue(vcs.VcsType)
	state.ConnectionType = types.StringValue(vcs.ConnectionType)
	state.ClientId = types.StringValue(vcs.ClientId)
	state.Endpoint = types.StringValue(vcs.Endpoint)
	state.ApiUrl = types.StringValue(vcs.ApiUrl)
	state.Status = types.StringValue(vcs.Status)

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if vcs.Status == "PENDING" {
		tflog.Warn(ctx, fmt.Sprintf("VCS connection is pending, please logon to %s to connect. Check doc here %s", state.ConnectUrl, helpers.GetVCSProviderDoc()))
	}

	tflog.Info(ctx, "VCS Resource read succeed", map[string]any{"success": true})
}

func (r *VcsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan VcsResourceModel
	var state VcsResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.VcsEntity{
		ID:             plan.ID.ValueString(),
		Name:           plan.Name.ValueString(),
		Description:    plan.Description.ValueStringPointer(),
		VcsType:        plan.VcsType.ValueString(),
		ConnectionType: plan.ConnectionType.ValueString(),
		ClientId:       plan.ClientId.ValueString(),
		ClientSecret:   plan.ClientSecret.ValueString(),
		PrivateKey:     plan.PrivateKey.ValueString(),
		Endpoint:       plan.Endpoint.ValueString(),
		ApiUrl:         plan.ApiUrl.ValueString(),
		Status:         plan.Status.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	tflog.Info(ctx, "Body Update Request: "+out.String())

	vcsRequest, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/vcs/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), strings.NewReader(out.String()))
	vcsRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	vcsRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating VCS resource request", fmt.Sprintf("Error creating VCS resource request: %s", err))
		return
	}

	vcsResponse, err := r.client.Do(vcsRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing VCS resource request", fmt.Sprintf("Error executing VCS resource request, response status %s, response body %s, error: %s", vcsResponse.Status, vcsResponse.Request.Body, err))
		return
	}

	bodyResponse, err := io.ReadAll(vcsResponse.Body)
	if err != nil {
		tflog.Error(ctx, fmt.Sprintf("Error reading organization variable resource response, error %s, response status %s", err, vcsResponse.Status))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"success": string(bodyResponse)})

	vcsRequest, err = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/vcs/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	vcsRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	vcsRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating VCS resource request", fmt.Sprintf("Error creating VCS resource request: %s", err))
		return
	}

	vcsResponse, err = r.client.Do(vcsRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing VCS resource request", fmt.Sprintf("Error executing VCS resource request, response status %s, response body %s, error: %s", vcsResponse.Status, vcsResponse.Request.Body, err))
		return
	}

	bodyResponse, err = io.ReadAll(vcsResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading VCS resource response body", fmt.Sprintf("Error reading VCS resource response body, error: %s, response status %s", err, vcsResponse.Status))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})
	vcs := &client.VcsEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), vcs)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response, error: %s, response status: %s", err, vcsResponse.Status))
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.Name = types.StringValue(vcs.Name)
	plan.Description = types.StringPointerValue(vcs.Description)
	plan.ConnectionType = types.StringValue(vcs.ConnectionType)
	plan.VcsType = types.StringValue(vcs.VcsType)
	plan.ClientId = types.StringValue(vcs.ClientId)
	if plan.ClientSecret.ValueString() != "" {
		tflog.Info(ctx, "Client secret is not available in the response, setting to original value set in the request.")
		plan.ClientSecret = types.StringValue(plan.ClientSecret.ValueString())
	}
	if plan.PrivateKey.ValueString() != "" {
		tflog.Info(ctx, "Private key is not available in the response, setting to original value set in the request.")
		plan.PrivateKey = types.StringValue(plan.PrivateKey.ValueString())
	}
	plan.Endpoint = types.StringValue(vcs.Endpoint)
	plan.ApiUrl = types.StringValue(vcs.ApiUrl)
	plan.Status = types.StringValue(vcs.Status)
	plan.ConnectUrl = types.StringValue(plan.ConnectUrl.ValueString())

	if vcs.Status == "PENDING" {
		tflog.Warn(ctx, fmt.Sprintf("VCS connection is pending, please logon to %s to connect. Check doc here %s", plan.ConnectUrl, helpers.GetVCSProviderDoc()))
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *VcsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data VcsResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	vcsRequest, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/organization/%s/vcs/%s", r.endpoint, data.OrganizationId.ValueString(), data.ID.ValueString()), nil)
	vcsRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	if err != nil {
		resp.Diagnostics.AddError("Error creating VCS resource request", fmt.Sprintf("Error creating VCS resource request: %s", err))
		return
	}

	vcsResponse, err := r.client.Do(vcsRequest)
	if err != nil || vcsResponse.StatusCode != http.StatusNoContent {
		resp.Diagnostics.AddError("Error executing VCS resource request", fmt.Sprintf("Error executing VCS resource request, response status %s, response body %s, error: %s", vcsResponse.Status, vcsResponse.Request.Body, err))
		return
	}
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
