variable "name" {
  type = string
}

variable "description" {
  type = string
}

variable "branch" {
  type = string
}

variable "folder" {
  type = string
}

resource "terrakube_organization" "test" {
  name           = var.name
  execution_mode = "local"
}

# The admin group can create organizations, but managing workspaces and templates in one needs a
# team with manage_workspace and manage_template in that organization.
resource "terrakube_team" "admins" {
  organization_id  = terrakube_organization.test.id
  name             = "TERRAKUBE_ADMIN"
  manage_workspace = true
  manage_template  = true
}

resource "terrakube_organization_template" "plan" {
  depends_on      = [terrakube_team.admins]
  organization_id = terrakube_organization.test.id
  name            = "plan"
  content         = <<-EOT
    flow:
      - type: "terraformPlan"
        name: "Plan"
        step: 100
  EOT
}

# Terrakube doesn't clone the repository when the workspace is created, so it doesn't need to exist.
resource "terrakube_workspace_vcs" "test" {
  organization_id = terrakube_organization.test.id
  name            = var.name
  description     = var.description
  repository      = "https://github.com/example/infrastructure.git"
  branch          = var.branch
  folder          = var.folder
  template_id     = terrakube_organization_template.plan.id
  execution_mode  = "local"
  iac_type        = "tofu"
  iac_version     = "1.12.5"
}
