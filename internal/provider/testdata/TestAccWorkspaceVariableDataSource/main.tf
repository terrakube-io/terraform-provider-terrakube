variable "name" {
  type = string
}

resource "terrakube_organization" "test" {
  name           = var.name
  description    = "Acceptance test organization"
  execution_mode = "local"
}

# The admin group can create organizations, but managing objects in one needs a team with the
# admin group's name and the matching manage_* permissions.
resource "terrakube_team" "admins" {
  organization_id   = terrakube_organization.test.id
  name              = "TERRAKUBE_ADMIN"
  manage_workspace  = true
  manage_template   = true
  manage_collection = true
  manage_vcs        = true
  manage_module     = true
  manage_provider   = true
  manage_state      = true
  manage_job        = true
}

resource "terrakube_workspace_cli" "test" {
  depends_on      = [terrakube_team.admins]
  organization_id = terrakube_organization.test.id
  name            = "${var.name}-workspace"
  description     = "Acceptance test workspace"
  execution_mode  = "local"
  iac_type        = "tofu"
  iac_version     = "1.12.5"
}

resource "terrakube_workspace_variable" "test" {
  organization_id = terrakube_organization.test.id
  workspace_id    = terrakube_workspace_cli.test.id
  key             = "acc_variable"
  value           = "acceptance-value"
  description     = "Acceptance test variable"
  category        = "TERRAFORM"
  sensitive       = false
  hcl             = false
}

data "terrakube_workspace_variable" "test" {
  organization_id = terrakube_organization.test.id
  workspace_id    = terrakube_workspace_cli.test.id
  key             = terrakube_workspace_variable.test.key
}

# Terrakube never returns the value of a sensitive variable, so the data source reads it as null.
resource "terrakube_workspace_variable" "secret" {
  organization_id = terrakube_organization.test.id
  workspace_id    = terrakube_workspace_cli.test.id
  key             = "acc_secret"
  value           = "acceptance-secret"
  description     = "Acceptance test sensitive variable"
  category        = "TERRAFORM"
  sensitive       = true
  hcl             = false
}

data "terrakube_workspace_variable" "secret" {
  organization_id = terrakube_organization.test.id
  workspace_id    = terrakube_workspace_cli.test.id
  key             = terrakube_workspace_variable.secret.key
}
