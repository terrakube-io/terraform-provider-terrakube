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
