variable "name" {
  type = string
}

variable "value" {
  type      = string
  sensitive = true
}

variable "value_version" {
  type = number
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
  execution_mode  = "local"
  iac_type        = "tofu"
  iac_version     = "1.12.5"
}

# value_wo is never stored in state. Terrakube only gets a new value when value_wo_version changes.
resource "terrakube_workspace_variable" "test" {
  organization_id  = terrakube_organization.test.id
  workspace_id     = terrakube_workspace_cli.test.id
  key              = "acc_write_only"
  value_wo         = var.value
  value_wo_version = var.value_version
  description      = "Acceptance test write-only variable"
  category         = "TERRAFORM"
  sensitive        = true
  hcl              = false
}
