resource "terrakube_workspace_access" "workspace_access" {
  name            = "my_terrakube_team"
  organization_id = "my_organization_id"
  workspace_id    = "my_workspace_id"

  manage_job       = true # legacy field — plan_job/approve_job inherit from this when unset
  manage_state     = false
  manage_workspace = false
  plan_job         = true     # RBAC v2: required for "New Run" button
  approve_job      = true     # RBAC v2: required for apply approval
  role             = "custom" # "custom" defers to boolean flags
}
