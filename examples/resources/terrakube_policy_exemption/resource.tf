data "terrakube_organization" "org" {
  name = "my-organization"
}

data "terrakube_policy_set" "blast_radius" {
  organization = data.terrakube_organization.org.name
  name         = "blast-radius-guardrails"
}

resource "terrakube_policy_exemption" "apim_gateway_exemption" {
  organization_id  = data.terrakube_organization.org.id
  policy_set_id    = data.terrakube_policy_set.blast_radius.id
  rule_id          = "azure_apim_no_public_network"
  ticket_reference = "SEC-8842"
  justification    = "Approved third-party integration gateway for Partner Corp per architectural review"
  expires_at       = "2026-12-31T23:59:59Z"
  workspace_id     = "22222222-2222-2222-2222-222222222222"
}
