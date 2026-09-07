data "terrakube_policy_exemption" "by_rule" {
  organization = "enterprise"
  rule_id      = "azure_apim_no_public_network"
}

output "exemption_expires_at" {
  value = data.terrakube_policy_exemption.by_rule.expires_at
}
