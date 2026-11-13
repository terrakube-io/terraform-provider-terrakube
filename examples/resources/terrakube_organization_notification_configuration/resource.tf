resource "terrakube_organization_notification_configuration" "prod_alerts" {
  organization_id  = data.terrakube_organization.org.id
  name             = "prod-alerts"
  description      = "Pages on-call for failures across every workspace"
  channel_type     = "SLACK"
  destination_url  = "https://hooks.slack.com/services/XXX/YYY/ZZZ"
  active           = true
  message_style    = "DETAILED"
  trigger_statuses = ["failed", "rejected", "cancelled"]
  # Empty/omitted template_ids (the default) means this applies to every template.
}

# Generic webhook signed with a write-only secret (Terraform 1.11+ / OpenTofu
# 1.11+): sent to Terrakube but never stored in the plan or state. Bump
# signing_secret_wo_version to push a new secret.
ephemeral "random_password" "webhook_signing" {
  length = 32
}

resource "terrakube_organization_notification_configuration" "signed_webhook" {
  organization_id           = data.terrakube_organization.org.id
  name                      = "signed-webhook"
  channel_type              = "WEBHOOK"
  destination_url           = "https://example.com/terrakube-hook"
  signing_secret_wo         = ephemeral.random_password.webhook_signing.result
  signing_secret_wo_version = 1
  trigger_statuses          = ["failed"]
}
