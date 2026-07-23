resource "terrakube_project_access" "admins" {
  name            = "my_terrakube_team"
  organization_id = "my_organization_id"
  project_id      = "my_project_id"
  role            = "admin" # all permissions granted by role
}
