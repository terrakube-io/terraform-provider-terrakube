data "terrakube_organization" "org" {
  name = "my-organization"
}

data "terrakube_workspace" "ws" {
  name         = "my-workspace"
  organization = data.terrakube_organization.org.name
}

data "terrakube_organization_template" "plan" {
  name            = "plan"
  organization_id = data.terrakube_organization.org.id
}

# Run the template every day at 02:00. The schedule uses Quartz cron notation.
resource "terrakube_workspace_schedule" "nightly" {
  organization_id = data.terrakube_organization.org.id
  workspace_id    = data.terrakube_workspace.ws.id
  template_id     = data.terrakube_organization_template.plan.id
  schedule        = "0 0 2 * * ?"
}
