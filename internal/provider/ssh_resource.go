package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
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
	terrakube "github.com/terrakube-io/terrakube-go"
)

var _ resource.Resource = &SshResource{}
var _ resource.ResourceWithImportState = &SshResource{}
var _ resource.ResourceWithConfigValidators = &SshResource{}

type SshResource struct {
	client *terrakube.Client
}

type SshResourceModel struct {
	ID                  types.String `tfsdk:"id"`
	Name                types.String `tfsdk:"name"`
	OrganizationId      types.String `tfsdk:"organization_id"`
	Description         types.String `tfsdk:"description"`
	PrivateKey          types.String `tfsdk:"private_key"`
	PrivateKeyWo        types.String `tfsdk:"private_key_wo"`
	PrivateKeyWoVersion types.Int64  `tfsdk:"private_key_wo_version"`
	SshType             types.String `tfsdk:"ssh_type"`
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
				Description: "Ssh key name. Changing this forces a new resource.",
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
				Description: "SSH Key content, stored in the Terraform state. Exactly one of `private_key` or `private_key_wo` is required.",
			},
			"private_key_wo":         writeOnlyAttribute("private_key", "SSH Key content", false),
			"private_key_wo_version": writeOnlyVersionAttribute("private_key"),
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
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Ssh Key Resource")
}

func (r *SshResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SshResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	privateKey, _, diags := writeOnlyValue(ctx, req.Config, plan.PrivateKey, "private_key")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The key comes from private_key or private_key_wo, exactly one of which is set, so it is always sent.
	attributes := patchAttributes{"privateKey": privateKey}
	attributes.createString("name", plan.Name)
	attributes.createString("description", plan.Description)
	attributes.createString("sshType", plan.SshType)

	sshKey, err := r.client.SSH.CreateFields(ctx, plan.OrganizationId.ValueString(), terrakube.Patch{Attributes: attributes})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating ssh key", err)
		return
	}

	plan.ID = types.StringValue(sshKey.ID)
	plan.Name = types.StringValue(sshKey.Name)
	// private_key stays as planned (null with private_key_wo): Terrakube never returns it.
	plan.PrivateKeyWo = types.StringNull()
	plan.SshType = types.StringValue(sshKey.SSHType)
	plan.Description = types.StringPointerValue(sshKey.Description)

	tflog.Info(ctx, "Ssh Key Resource Created", map[string]any{"success": true})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SshResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SshResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sshKey, err := r.client.SSH.Get(ctx, state.OrganizationId.ValueString(), state.ID.ValueString())
	if terrakube.IsNotFound(err) {
		tflog.Warn(ctx, "SSH key not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading ssh key", err)
		return
	}

	state.Name = types.StringValue(sshKey.Name)
	// private_key is kept from the state: Terrakube never returns it ("read
	// private key" permission). Null stays null, so private_key_wo shows no diff.
	state.SshType = types.StringValue(sshKey.SSHType)
	state.Description = types.StringPointerValue(sshKey.Description)
	state.ID = types.StringValue(sshKey.ID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *SshResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan SshResourceModel
	var state SshResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The key is always sent: from private_key, or from private_key_wo in the
	// configuration, which is null in both plan and state.
	privateKey, _, diags := writeOnlyValue(ctx, req.Config, plan.PrivateKey, "private_key")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	attributes := patchAttributes{"privateKey": privateKey}
	attributes.updateString("name", plan.Name, state.Name)
	attributes.updateString("description", plan.Description, state.Description)
	attributes.updateString("sshType", plan.SshType, state.SshType)

	organizationID, id := state.OrganizationId.ValueString(), state.ID.ValueString()
	if _, err := r.client.SSH.UpdateFields(ctx, organizationID, id, terrakube.Patch{Attributes: attributes}); patchFailed(err) {
		reportClientError(&resp.Diagnostics, "updating ssh key", err)
		return
	}

	sshKey, err := r.client.SSH.Get(ctx, organizationID, id)
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading ssh key after update", err)
		return
	}

	plan.ID = types.StringValue(id)
	plan.Name = types.StringValue(sshKey.Name)
	plan.Description = types.StringPointerValue(sshKey.Description)
	plan.PrivateKeyWo = types.StringNull()
	plan.SshType = types.StringValue(sshKey.SSHType)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SshResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data SshResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.SSH.Delete(ctx, data.OrganizationId.ValueString(), data.ID.ValueString())
	if err != nil && !terrakube.IsNotFound(err) {
		reportClientError(&resp.Diagnostics, "deleting ssh key", err)
	}
}

func (r *SshResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.ExactlyOneOf(
			path.MatchRoot("private_key"),
			path.MatchRoot("private_key_wo"),
		),
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
