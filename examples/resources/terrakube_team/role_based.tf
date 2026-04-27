resource "terrakube_team" "admins" {
  name            = "TERRAKUBE_ADMINS"
  organization_id = data.terrakube_organization.org.id
  role            = "admin" # all permissions granted by role
}
