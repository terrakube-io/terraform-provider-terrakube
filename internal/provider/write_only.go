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

// Write-only arguments follow the <attr>_wo / <attr>_wo_version convention:
// <attr>_wo is sent to Terrakube but never stored in the plan or state, and
// <attr>_wo_version is the only way to make Terraform send a new value.

// writeOnlyAttribute is the <attr>_wo argument. description says what it holds,
// e.g. "Variable value"; extra lists other requirements before the Terraform
// version, e.g. "`sensitive = true` and ".
func writeOnlyAttribute(attr, description, extra string) schema.StringAttribute {
	return schema.StringAttribute{
		Optional:  true,
		Sensitive: true,
		WriteOnly: true,
		Description: fmt.Sprintf("%s as a write-only argument: sent to Terrakube but never stored in the plan or state. Requires %sTerraform 1.11+ or OpenTofu 1.11+. Terraform cannot detect changes to it: bump `%s_wo_version` to send a new value.",
			description, extra, attr),
	}
}

// writeOnlyVersionAttribute is the <attr>_wo_version argument.
func writeOnlyVersionAttribute(attr string) schema.Int64Attribute {
	return schema.Int64Attribute{
		Optional:    true,
		Description: fmt.Sprintf("Version of `%s_wo`. Change it to update the value in Terrakube.", attr),
		Validators: []validator.Int64{
			int64validator.AlsoRequires(path.MatchRoot(attr + "_wo")),
		},
	}
}

// writeOnlyValue returns the value to send to Terrakube for attr: attr from the
// plan, or attr_wo from the configuration (write-only values are never in the
// plan, but are in the configuration on every apply).
func writeOnlyValue(ctx context.Context, config tfsdk.Config, value types.String, attr string) (string, bool, diag.Diagnostics) {
	if !value.IsNull() {
		return value.ValueString(), false, nil
	}

	var valueWo types.String
	diags := config.GetAttribute(ctx, path.Root(attr+"_wo"), &valueWo)
	return valueWo.ValueString(), !valueWo.IsNull(), diags
}

// Shared value / value_wo / value_wo_version handling for the resources that
// hold a variable value (collection items, workspace and organization variables).

const valueDescription = "Variable value, stored in the Terraform state. Exactly one of `value` or `value_wo` is required."

func valueWoAttribute() schema.StringAttribute {
	return writeOnlyAttribute("value", "Variable value", "`sensitive = true` and ")
}

func valueWoVersionAttribute() schema.Int64Attribute {
	return writeOnlyVersionAttribute("value")
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

	addValueWoNotSensitiveError(&diags, kind)
	return diags
}

// valueOrWriteOnly returns the value to send to Terrakube: value from the plan,
// or value_wo from the configuration.
// It repeats the sensitive check of validateValueWoSensitive, which skips an
// unknown sensitive at validate time; at apply it is known.
func valueOrWriteOnly(ctx context.Context, config tfsdk.Config, value types.String, sensitive types.Bool, kind string) (string, diag.Diagnostics) {
	v, fromWo, diags := writeOnlyValue(ctx, config, value, "value")
	if diags.HasError() {
		return "", diags
	}

	if fromWo && !sensitive.ValueBool() {
		addValueWoNotSensitiveError(&diags, kind)
		return "", diags
	}

	return v, diags
}

func addValueWoNotSensitiveError(diags *diag.Diagnostics, kind string) {
	diags.AddAttributeError(
		path.Root("value_wo"),
		"value_wo requires sensitive = true",
		fmt.Sprintf("Terrakube returns the value of a non-sensitive %s, so it cannot be kept out of the state. Set sensitive = true, or use value.", kind),
	)
}
