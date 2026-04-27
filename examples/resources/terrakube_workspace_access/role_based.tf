resource "terrakube_workspace_access" "writers" {
  name            = "my_terrakube_team"
  organization_id = "my_organization_id"
  workspace_id    = "my_workspace_id"
  role            = "write" # plan+apply+workspace+state permissions granted by role
}
