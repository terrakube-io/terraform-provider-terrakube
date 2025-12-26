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
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ resource.Resource = &SshResource{}
var _ resource.ResourceWithImportState = &SshResource{}

type SshResource struct {
	client   *http.Client
	endpoint string
	token    string
}

type SshResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	OrganizationId types.String `tfsdk:"organization_id"`
	Description    types.String `tfsdk:"description"`
	PrivateKey     types.String `tfsdk:"private_key"`
	SshType        types.String `tfsdk:"ssh_type"`
}

func NewSshResource() resource.Resource {
	return &SshResource{}
}

func (r *SshResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh"
}

func (r *SshResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a SSH key for the desired organization. SSH key  are used by download private modules with Git-based sources during Terraform execution ",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "SsH key ID",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization_id": schema.StringAttribute{
				Required:    true,
				Description: "Terrakube organization ID",
			},
			"name": schema.StringAttribute{
				Optional:    true,
				Description: "Ssh key name",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "SSH key description",
			},
			"private_key": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "SSH Key content",
			},
			"ssh_type": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("rsa"),
				Description: "SSH key type",
				Validators: []validator.String{
					stringvalidator.OneOf("rsa", "ed25519"),
				},
			},
		},
	}
}

func (r *SshResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*TerrakubeConnectionData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Ssh Key Resource Configure Type",
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

	tflog.Debug(ctx, "Configuring Ssh Key resource", map[string]any{"success": true})

}

func (r *SshResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SshResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.SshEntity{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueStringPointer(),
		PrivateKey:  plan.PrivateKey.ValueString(),
		SshType:     plan.SshType.ValueString(),
	}

	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}
	sshRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/organization/%s/ssh", r.endpoint, plan.OrganizationId.ValueString()), strings.NewReader(out.String()))
	sshRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	sshRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating ssh key resource request", fmt.Sprintf("Error creating ssh key resource request: %s", err))
		return
	}

	sshResponse, err := r.client.Do(sshRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing ssh key resource request", fmt.Sprintf("Error executing ssh key resource request: %s", err))
		return
	}
	bodyResponse, err := io.ReadAll(sshResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading ssh key resource response")
	}
	newSshKey := &client.SshEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), newSshKey)
	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	plan.ID = types.StringValue(newSshKey.ID)
	plan.Name = types.StringValue(newSshKey.Name)
	plan.PrivateKey = types.StringValue(plan.PrivateKey.ValueString())
	plan.SshType = types.StringValue(newSshKey.SshType)
	plan.Description = types.StringPointerValue(newSshKey.Description)
	tflog.Info(ctx, "Ssh Key Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)

}

func (r *SshResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SshResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	sshRequest, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/ssh/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	sshRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	sshRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating ssh key resource request", fmt.Sprintf("Error creating ssh key resource request: %s", err))
		return
	}

	sshResponse, err := r.client.Do(sshRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing ssh key resource request", fmt.Sprintf("Error executing ssh key resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(sshResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading ssh key resource response")
	}
	sshKey := &client.SshEntity{}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), sshKey)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})
	state.Name = types.StringValue(sshKey.Name)
	state.PrivateKey = types.StringValue(state.PrivateKey.ValueString()) //value is not inside the response getting the value from the current state
	state.SshType = types.StringValue(sshKey.SshType)
	state.Description = types.StringPointerValue(sshKey.Description)
	state.ID = types.StringValue(sshKey.ID)

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Ssh Key Resource reading", map[string]any{"success": true})

}

func (r *SshResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan SshResourceModel
	var state SshResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bodyRequest := &client.SshEntity{
		ID:          plan.ID.ValueString(),
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueStringPointer(),
		PrivateKey:  plan.PrivateKey.ValueString(),
		SshType:     plan.SshType.ValueString(),
	}
	var out = new(bytes.Buffer)
	err := jsonapi.MarshalPayload(out, bodyRequest)

	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return
	}

	sshRequest, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/organization/%s/ssh/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), strings.NewReader(out.String()))
	sshRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	sshRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating ssh key resource request", fmt.Sprintf("Error creating ssh key resource request: %s", err))
		return
	}
	sshResponse, err := r.client.Do(sshRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing ssh key resource request", fmt.Sprintf("Error executing ssh key resource request: %s", err))
		return
	}

	bodyResponse, err := io.ReadAll(sshResponse.Body)
	if err != nil {
		tflog.Error(ctx, "Error reading ssh key resource response")
	}
	tflog.Info(ctx, "Body Response", map[string]any{"success": string(bodyResponse)})

	sshRequest, err = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/organization/%s/ssh/%s", r.endpoint, state.OrganizationId.ValueString(), state.ID.ValueString()), nil)
	sshRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	sshRequest.Header.Add("Content-Type", "application/vnd.api+json")
	if err != nil {
		resp.Diagnostics.AddError("Error creating ssh key resource request", fmt.Sprintf("Error creating ssh key resource request: %s", err))
		return
	}

	sshResponse, err = r.client.Do(sshRequest)
	if err != nil {
		resp.Diagnostics.AddError("Error executing ssh key resource request", fmt.Sprintf("Error executing ssh key resource request: %s", err))
		return
	}

	bodyResponse, err = io.ReadAll(sshResponse.Body)
	if err != nil {
		resp.Diagnostics.AddError("Error reading ssh key resource response body", fmt.Sprintf("Error reading ssh key resource response body: %s", err))
	}

	tflog.Info(ctx, "Body Response", map[string]any{"bodyResponse": string(bodyResponse)})

	ssh := &client.SshEntity{}
	err = jsonapi.UnmarshalPayload(strings.NewReader(string(bodyResponse)), ssh)

	if err != nil {
		resp.Diagnostics.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return
	}

	plan.ID = types.StringValue(state.ID.ValueString())
	plan.Name = types.StringValue(ssh.Name)
	plan.Description = types.StringPointerValue(ssh.Description)
	plan.PrivateKey = types.StringValue(plan.PrivateKey.ValueString())
	plan.SshType = types.StringValue(ssh.SshType)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)

}

func (r *SshResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data SshResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	reqOrg, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/organization/%s/ssh/%s", r.endpoint, data.OrganizationId.ValueString(), data.ID.ValueString()), nil)
	reqOrg.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.token))
	if err != nil {
		resp.Diagnostics.AddError("Error creating ssh key resource request", fmt.Sprintf("Error creating ssh key resource request: %s", err))
		return
	}

	_, err = r.client.Do(reqOrg)
	if err != nil {
		resp.Diagnostics.AddError("Error executing ssh key resource request", fmt.Sprintf("Error executing ssh key resource request: %s", err))
		return
	}
}

func (r *SshResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
