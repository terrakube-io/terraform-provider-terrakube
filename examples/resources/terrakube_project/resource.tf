resource "terrakube_project" "project" {
  name            = "sample-project"
  organization_id = data.terrakube_organization.org.id
  description     = "Groups workspaces for the sample application"
}
