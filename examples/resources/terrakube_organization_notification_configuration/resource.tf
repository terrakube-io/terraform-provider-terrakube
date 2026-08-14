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
