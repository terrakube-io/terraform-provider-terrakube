data "terrakube_policy_set" "guardrails" {
  organization = "my-organization"
  name         = "blast-radius-guardrails"
}

# Available in Rego as data.terrakube.inputs.max_resource_changes
resource "terrakube_policy_set_parameter" "max_changes" {
  policy_set_id = data.terrakube_policy_set.guardrails.id
  key           = "max_resource_changes"
  value         = "50"
  description   = "Maximum number of resources a single plan may change"
}
