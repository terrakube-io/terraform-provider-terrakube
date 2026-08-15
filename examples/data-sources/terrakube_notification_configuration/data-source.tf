data "terrakube_notification_configuration" "prod_alerts" {
  organization_id = data.terrakube_organization.org.id
  name            = "prod-alerts"
}
