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
  project_id      = terrakube_project.project.id

  # Optional: use an org SSH key to download private Terraform/OpenTofu
  # modules referenced via git-based module sources in this workspace.
  module_ssh_key = terrakube_ssh.module_key.id
}
