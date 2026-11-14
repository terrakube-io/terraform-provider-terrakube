data "terrakube_policy_set" "guardrails" {
  organization = "my-organization"
  name         = "blast-radius-guardrails"
}

# Look up the attachment of a policy set to a workspace.
data "terrakube_policy_attachment" "workspace" {
  policy_set_id = data.terrakube_policy_set.guardrails.id
  workspace_id  = "00000000-0000-0000-0000-000000000000"
}

output "policy_attachment_id" {
  value = data.terrakube_policy_attachment.workspace.id
}
