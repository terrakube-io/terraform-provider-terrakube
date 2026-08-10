resource "terrakube_workspace_vcs" "sample1" {
  organization_id = data.terrakube_organization.org.id
  name            = "work-from-provider1"
  description     = "sample"
  execution_mode  = "remote"
  repository      = "https://github.com/terrakube-io/terrakube-docker-compose.git"
  branch          = "main"
  folder          = "/"
  template_id     = terrakube_organization_template.example.id
  iac_type        = "terraform"
  iac_version     = "1.5.7"
  project_id      = terrakube_project.project.id

  # Optional: use an org SSH key to download private Terraform/OpenTofu
  # modules referenced via git-based module sources in this workspace.
  module_ssh_key = terrakube_ssh.module_key.id
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
  template_id     = terrakube_organization_template.example.id
  iac_type        = "terraform"
  iac_version     = "1.5.7"
  ssh_id          = terrakube_ssh.workspace_key.id
}
