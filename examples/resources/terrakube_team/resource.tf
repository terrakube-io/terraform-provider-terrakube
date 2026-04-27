resource "terrakube_team" "team" {
  name              = "TERRAKUBE_SUPER_ADMIN"
  organization_id   = data.terrakube_organization.org.id
  manage_state      = false
  manage_workspace  = false
  manage_module     = false
  manage_provider   = true
  manage_vcs        = true
  manage_template   = true
  manage_job        = true # legacy field — plan_job/approve_job inherit from this when unset
  manage_collection = true
  plan_job          = true     # RBAC v2: required for "New Run" button
  approve_job       = true     # RBAC v2: required for apply approval
  role              = "custom" # "custom" defers to boolean flags
}
