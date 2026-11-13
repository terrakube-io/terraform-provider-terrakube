resource "terrakube_workspace_notification_configuration" "security_team_slack" {
  organization_id  = data.terrakube_organization.org.id
  workspace_id     = data.terrakube_workspace.security.id
  name             = "security-team-slack"
  channel_type     = "SLACK"
  destination_url  = "https://hooks.slack.com/services/AAA/BBB/CCC"
  active           = true
  message_style    = "SIMPLE"
  trigger_statuses = ["failed", "waitingApproval"]
  # Narrow to specific templates; omit (or leave empty) to apply to every template.
  template_ids = [data.terrakube_organization_template.security_scan.id]
}

# Generic webhook signed with a write-only secret (Terraform 1.11+ / OpenTofu
# 1.11+): sent to Terrakube but never stored in the plan or state. Bump
# signing_secret_wo_version to push a new secret.
ephemeral "random_password" "webhook_signing" {
  length = 32
}

resource "terrakube_workspace_notification_configuration" "signed_webhook" {
  organization_id           = data.terrakube_organization.org.id
  workspace_id              = data.terrakube_workspace.security.id
  name                      = "signed-webhook"
  channel_type              = "WEBHOOK"
  destination_url           = "https://example.com/terrakube-hook"
  signing_secret_wo         = ephemeral.random_password.webhook_signing.result
  signing_secret_wo_version = 1
  trigger_statuses          = ["failed"]
}
