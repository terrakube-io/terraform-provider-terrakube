resource "terrakube_policy_exemption" "apim_gateway_exemption" {
  organization_id  = "00000000-0000-0000-0000-000000000000"
  policy_set_id    = terrakube_policy_set.blast_radius.id
  rule_id          = "azure_apim_no_public_network"
  ticket_reference = "SEC-8842"
  justification    = "Approved third-party integration gateway for Partner Corp per architectural review"
  expires_at       = "2026-12-31T23:59:59Z"
  workspace_id     = "22222222-2222-2222-2222-222222222222"
}
