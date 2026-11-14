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

data "terrakube_self_hosted_agent" "agent" {
  organization_id = data.terrakube_organization.org.id
  name            = "MY-SUPER-AGENT"
}

resource "terrakube_workspace_cli" "sample1" {
  organization_id = data.terrakube_organization.org.id
  name            = "work-from-provider1"
  description     = "sample"
  execution_mode  = "remote"
  iac_type        = "terraform"
  iac_version     = "1.5.7"
}

resource "terrakube_workspace_cli" "sample2" {
  organization_id = data.terrakube_organization.org.id
  name            = "work-from-provider2"
  description     = "sample"
  execution_mode  = "local"
  iac_type        = "tofu"
  iac_version     = "1.7.0"
}

resource "terrakube_workspace_cli" "sample3" {
  organization_id = data.terrakube_organization.org.id
  name            = "work-from-provider3"
  description     = "sample workspace assigned to a project"
  execution_mode  = "remote"
  iac_type        = "terraform"
  iac_version     = "1.5.7"
  project_id      = data.terrakube_project.project.id

  # Terrakube 2.34.0+: require approval permission for applies without an approval step.
  require_approve_to_apply = true

  # Optional: use an org SSH key to download private Terraform/OpenTofu
  # modules referenced via git-based module sources in this workspace.
  module_ssh_key = data.terrakube_ssh.module_key.id

  # Optional: pin this workspace's remote runs to a specific self-hosted
  # executor agent instead of the organization's default executor.
  agent_id = data.terrakube_self_hosted_agent.agent.id
}
