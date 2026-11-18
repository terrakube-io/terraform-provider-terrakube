variable "name" {
  type = string
}

variable "value" {
  type = string
}

variable "description" {
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
  execution_mode  = "local"
  iac_type        = "tofu"
  iac_version     = "1.12.5"
}

resource "terrakube_workspace_variable" "test" {
  organization_id = terrakube_organization.test.id
  workspace_id    = terrakube_workspace_cli.test.id
  key             = "acc_variable"
  value           = var.value
  description     = var.description
  category        = "TERRAFORM"
  sensitive       = false
  hcl             = false
}

# Terrakube never returns the value of a sensitive variable, so the provider keeps the configured one.
resource "terrakube_workspace_variable" "secret" {
  organization_id = terrakube_organization.test.id
  workspace_id    = terrakube_workspace_cli.test.id
  key             = "acc_secret"
  value           = var.value
  description     = var.description
  category        = "TERRAFORM"
  sensitive       = true
  hcl             = false
}
