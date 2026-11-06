package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Shared value / value_wo / value_wo_version handling for the resources that
// hold a variable value (collection items, workspace and organization variables).

const valueDescription = "Variable value, stored in the Terraform state. Exactly one of `value` or `value_wo` is required."

func valueWoAttribute() schema.StringAttribute {
	return schema.StringAttribute{
		Optional:    true,
		Sensitive:   true,
		WriteOnly:   true,
		Description: "Variable value as a write-only argument: sent to Terrakube but never stored in the plan or state. Requires `sensitive = true` and Terraform 1.11+ or OpenTofu 1.11+. Terraform cannot detect changes to it: bump `value_wo_version` to send a new value.",
	}
}

func valueWoVersionAttribute() schema.Int64Attribute {
	return schema.Int64Attribute{
		Optional:    true,
		Description: "Version of `value_wo`. Change it to update the value in Terrakube.",
		Validators: []validator.Int64{
			int64validator.AlsoRequires(path.MatchRoot("value_wo")),
		},
	}
}

func valueWoConfigValidators() []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.ExactlyOneOf(
			path.MatchRoot("value"),
			path.MatchRoot("value_wo"),
		),
	}
}

// validateValueWoSensitive requires sensitive = true with value_wo: Terrakube
// returns the value of a non-sensitive variable, so it could not stay out of the
// state. kind names the resource in the error, e.g. "collection item".
func validateValueWoSensitive(ctx context.Context, config tfsdk.Config, kind string) diag.Diagnostics {
	var valueWo types.String
	var sensitive types.Bool
	diags := config.GetAttribute(ctx, path.Root("value_wo"), &valueWo)
	diags.Append(config.GetAttribute(ctx, path.Root("sensitive"), &sensitive)...)
	if diags.HasError() {
		return diags
	}

	if valueWo.IsNull() || sensitive.IsUnknown() || sensitive.ValueBool() {
		return diags
	}

	diags.AddAttributeError(
		path.Root("value_wo"),
		"value_wo requires sensitive = true",
		fmt.Sprintf("Terrakube returns the value of a non-sensitive %s, so it cannot be kept out of the state. Set sensitive = true, or use value.", kind),
	)
	return diags
}

// valueOrWriteOnly returns the value to send to Terrakube: value from the plan,
// or value_wo from the configuration (write-only values are never in the plan).
func valueOrWriteOnly(ctx context.Context, config tfsdk.Config, value types.String) (string, diag.Diagnostics) {
	if !value.IsNull() {
		return value.ValueString(), nil
	}

	var valueWo types.String
	diags := config.GetAttribute(ctx, path.Root("value_wo"), &valueWo)
	return valueWo.ValueString(), diags
}
