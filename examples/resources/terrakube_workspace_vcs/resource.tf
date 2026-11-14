data "terrakube_organization" "org" {
  name = "my-organization"
}

data "terrakube_project" "project" {
  organization = data.terrakube_organization.org.name
  name         = "my-project"
}

data "terrakube_ssh" "module_key" {
  organization_id = data.terrakube_organization.org.id
  name            = "module-key"
}

data "terrakube_ssh" "workspace_key" {
  organization_id = data.terrakube_organization.org.id
  name            = "workspace-key"
}

data "terrakube_self_hosted_agent" "agent" {
  organization_id = data.terrakube_organization.org.id
  name            = "MY-SUPER-AGENT"
}

data "terrakube_organization_template" "plan" {
  name            = "plan"
  organization_id = data.terrakube_organization.org.id
}

resource "terrakube_workspace_vcs" "sample1" {
  organization_id = data.terrakube_organization.org.id
  name            = "work-from-provider1"
  description     = "sample"
  execution_mode  = "remote"
  repository      = "https://github.com/terrakube-io/terrakube-docker-compose.git"
  branch          = "main"
  folder          = "/"
  template_id     = data.terrakube_organization_template.plan.id
  iac_type        = "terraform"
  iac_version     = "1.5.7"
  project_id      = data.terrakube_project.project.id

  # Terrakube 2.34.0+ apply guards; requires organization workspace management.
  restrict_apply_to_configured_branch = true
  require_approve_to_apply            = true

  # Optional: use an org SSH key to download private Terraform/OpenTofu
  # modules referenced via git-based module sources in this workspace.
  module_ssh_key = data.terrakube_ssh.module_key.id

  # Optional: pin this workspace's remote runs to a specific self-hosted
  # executor agent instead of the organization's default executor.
  agent_id = data.terrakube_self_hosted_agent.agent.id
}

# Repository accessed over raw SSH instead of an OAuth VCS connection.
# vcs_id and ssh_id are mutually exclusive.
resource "terrakube_workspace_vcs" "sample2" {
  organization_id = data.terrakube_organization.org.id
  name            = "work-from-provider2"
  execution_mode  = "remote"
  repository      = "git@github.com:terrakube-io/terrakube-docker-compose.git"
  branch          = "main"
  folder          = "/"
  template_id     = data.terrakube_organization_template.plan.id
  iac_type        = "terraform"
  iac_version     = "1.5.7"
  ssh_id          = data.terrakube_ssh.workspace_key.id
}
