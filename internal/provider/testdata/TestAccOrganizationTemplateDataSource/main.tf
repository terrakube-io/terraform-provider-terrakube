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

resource "terrakube_organization_template" "test" {
  depends_on      = [terrakube_team.admins]
  organization_id = terrakube_organization.test.id
  name            = "${var.name}-template"
  description     = "Acceptance test template"
  version         = "1.0.0"
  content         = <<-YAML
    flow:
      - type: "terraformPlan"
        name: "Plan"
        step: 100
  YAML
}

data "terrakube_organization_template" "test" {
  organization_id = terrakube_organization.test.id
  name            = terrakube_organization_template.test.name
}
