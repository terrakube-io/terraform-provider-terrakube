data "terrakube_policy_set" "guardrails" {
  organization = "my-organization"
  name         = "blast-radius-guardrails"
}

data "terrakube_policy_set_parameter" "max_changes" {
  policy_set_id = data.terrakube_policy_set.guardrails.id
  key           = "max_resource_changes"
}

output "max_resource_changes" {
  value = data.terrakube_policy_set_parameter.max_changes.value
}
