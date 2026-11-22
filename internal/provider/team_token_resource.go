package provider

import (
	"context"
	"fmt"
	"terraform-provider-terrakube/internal/helpers"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int32planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	terrakube "github.com/terrakube-io/terrakube-go"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &TeamTokenResource{}
var _ resource.ResourceWithImportState = &TeamTokenResource{}

type TeamTokenResource struct {
	client *terrakube.Client
}

type TeamTokenResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Group       types.String `tfsdk:"team_name"`
	Description types.String `tfsdk:"description"`
	Days        types.Int32  `tfsdk:"days"`
	Hours       types.Int32  `tfsdk:"hours"`
	Minutes     types.Int32  `tfsdk:"minutes"`
	Value       types.String `tfsdk:"value"`
}

func NewTeamTokenResource() resource.Resource {
	return &TeamTokenResource{}
}

func (r *TeamTokenResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_team_token"
}

func (r *TeamTokenResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Create a team token that shares the same privilege. Useful when managing devs access.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Team Token Id",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"team_name": schema.StringAttribute{
				Required:    true,
				Description: "The name of the team who owns the token. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				Required:    true,
				Description: "A description of this token. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"days": schema.Int32Attribute{
				Required:    true,
				Description: "The number of days this token is valid for. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.Int32{
					int32planmodifier.RequiresReplace(),
				},
			},
			"hours": schema.Int32Attribute{
				Required:    true,
				Description: "The number of hours this token is valid for. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.Int32{
					int32planmodifier.RequiresReplace(),
				},
			},
			"minutes": schema.Int32Attribute{
				Required:    true,
				Description: "The number of minutes this token is valid for. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.Int32{
					int32planmodifier.RequiresReplace(),
				},
			},
			"value": schema.StringAttribute{
				Computed:    true,
				Description: "The value of the token.",
				Sensitive:   true,
			},
		},
	}
}

func (r *TeamTokenResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredClient(req.ProviderData, &resp.Diagnostics, "Team Token Resource")
}

func (r *TeamTokenResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan TeamTokenResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	newTeamToken, err := r.client.TeamTokens.Create(ctx, &terrakube.TeamToken{
		Description: plan.Description.ValueString(),
		Days:        plan.Days.ValueInt32(),
		Hours:       plan.Hours.ValueInt32(),
		Minutes:     plan.Minutes.ValueInt32(),
		Group:       plan.Group.ValueString(),
	})
	if err != nil {
		reportClientError(&resp.Diagnostics, "creating team token", err)
		return
	}

	id, err := helpers.GetIDFromToken(newTeamToken.Value)
	if err != nil {
		resp.Diagnostics.AddError("Error getting claim from token", fmt.Sprintf("Error getting claim from token: %s", err))
	}
	plan.ID = types.StringValue(id)
	plan.Value = types.StringValue(newTeamToken.Value)

	tflog.Info(ctx, "Team Token Resource Created", map[string]any{"success": true})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *TeamTokenResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state TeamTokenResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	teamTokens, err := r.client.TeamTokens.List(ctx)
	if err != nil {
		reportClientError(&resp.Diagnostics, "reading team tokens", err)
		return
	}

	found := false
	for _, teamToken := range teamTokens {
		if teamToken.ID != state.ID.ValueString() {
			continue
		}

		state.Description = types.StringValue(teamToken.Description)
		state.Days = types.Int32Value(teamToken.Days)
		state.Hours = types.Int32Value(teamToken.Hours)
		state.Minutes = types.Int32Value(teamToken.Minutes)
		state.Group = types.StringValue(teamToken.Group)
		found = true
		break
	}

	if !found {
		tflog.Warn(ctx, "Team token not found, removing from state", map[string]any{"id": state.ID.ValueString()})
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Team Token Resource reading finished", map[string]any{"success": true})
}

func (r *TeamTokenResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	tflog.Info(ctx, "Team token can't be updated but re-create.", map[string]any{"success": true})
}

func (r *TeamTokenResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data TeamTokenResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.TeamTokens.Delete(ctx, data.ID.ValueString())
	reportDeleteError(&resp.Diagnostics, "deleting team token", err)
}

func (r *TeamTokenResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
