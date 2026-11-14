data "terrakube_organization" "org" {
  name = "my-organization"
}

data "terrakube_workspace" "ws" {
  name         = "my-workspace"
  organization = data.terrakube_organization.org.name
}

data "terrakube_workspace_access" "platform" {
  organization_id = data.terrakube_organization.org.id
  workspace_id    = data.terrakube_workspace.ws.id
  name            = "platform-team"
}

output "platform_team_role" {
  value = data.terrakube_workspace_access.platform.role
}
