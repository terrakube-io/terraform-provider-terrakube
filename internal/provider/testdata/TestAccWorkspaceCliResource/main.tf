variable "name" {
  type = string
}

variable "description" {
  type = string
}

variable "iac_version" {
  type = string
}

variable "execution_mode" {
  type = string
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

resource "terrakube_workspace_cli" "test" {
  depends_on      = [terrakube_team.admins]
  organization_id = terrakube_organization.test.id
  name            = var.name
  description     = var.description
  execution_mode  = var.execution_mode
  iac_type        = "tofu"
  iac_version     = var.iac_version
}
