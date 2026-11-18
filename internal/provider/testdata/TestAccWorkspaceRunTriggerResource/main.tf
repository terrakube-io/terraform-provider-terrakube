variable "name" {
  type = string
}

variable "enabled" {
  type = bool
}

resource "terrakube_organization" "test" {
  name           = var.name
  execution_mode = "local"
}

# The admin group can create organizations, but managing workspaces in one needs a team with
# manage_workspace in that organization.
resource "terrakube_team" "admins" {
  organization_id  = terrakube_organization.test.id
  name             = "TERRAKUBE_ADMIN"
  manage_workspace = true
}

resource "terrakube_workspace_cli" "upstream" {
  depends_on      = [terrakube_team.admins]
  organization_id = terrakube_organization.test.id
  name            = "${var.name}-upstream"
  execution_mode  = "local"
  iac_type        = "tofu"
  iac_version     = "1.12.5"
}

resource "terrakube_workspace_cli" "downstream" {
  depends_on      = [terrakube_team.admins]
  organization_id = terrakube_organization.test.id
  name            = "${var.name}-downstream"
  execution_mode  = "local"
  iac_type        = "tofu"
  iac_version     = "1.12.5"
}

resource "terrakube_workspace_run_trigger" "test" {
  source_workspace_id      = terrakube_workspace_cli.upstream.id
  destination_workspace_id = terrakube_workspace_cli.downstream.id
  enabled                  = var.enabled
}
