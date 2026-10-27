data "terrakube_organization" "org" {
  name = "simple"
}

data "terrakube_workspace" "ws" {
  name         = "myworkspace"
  organization = "simple"
}

data "terrakube_workspace_tag" "example" {
  name            = "mytag"
  workspace_id    = data.terrakube_workspace.ws.id
  organization_id = data.terrakube_organization.org.id
}
