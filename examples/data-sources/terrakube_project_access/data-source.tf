data "terrakube_organization" "org" {
  name = "my-organization"
}

data "terrakube_project" "project" {
  organization = data.terrakube_organization.org.name
  name         = "my-project"
}

data "terrakube_project_access" "platform" {
  organization_id = data.terrakube_organization.org.id
  project_id      = data.terrakube_project.project.id
  name            = "platform-team"
}

output "platform_team_role" {
  value = data.terrakube_project_access.platform.role
}
