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

resource "terrakube_organization_tag" "test" {
  depends_on      = [terrakube_team.admins]
  organization_id = terrakube_organization.test.id
  name            = "${var.name}-tag"
}

resource "terrakube_workspace_tag" "test" {
  organization_id = terrakube_organization.test.id
  workspace_id    = terrakube_workspace_cli.test.id
  tag_id          = terrakube_organization_tag.test.id
}

data "terrakube_workspace_tag" "test" {
  depends_on      = [terrakube_workspace_tag.test]
  organization_id = terrakube_organization.test.id
  workspace_id    = terrakube_workspace_cli.test.id
  name            = terrakube_organization_tag.test.name
}
