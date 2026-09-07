data "terrakube_policy_set" "cis" {
  organization = "enterprise"
  name         = "enterprise-blast-radius-guardrails"
}

output "policy_set_enforcement_level" {
  value = data.terrakube_policy_set.cis.enforcement_level
}
