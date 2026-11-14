data "terrakube_organization" "org" {
  name = "my-organization"
}

data "terrakube_vcs" "vcs" {
  organization_id = data.terrakube_organization.org.id
  name            = "Github"
}

data "terrakube_ssh" "ssh" {
  organization_id = data.terrakube_organization.org.id
  name            = "github-key"
}

# Public repository.
resource "terrakube_module" "vpc" {
  name            = "vpc"
  organization_id = data.terrakube_organization.org.id
  description     = "cloudposse module"
  provider_name   = "aws"
  source          = "https://github.com/terraform-aws-modules/terraform-aws-vpc.git"
}

# Private repository through a VCS connection.
resource "terrakube_module" "vpc_private" {
  name            = "vpc_private"
  organization_id = data.terrakube_organization.org.id
  description     = "cloudposse module"
  provider_name   = "aws"
  source          = "https://github.com/terraform-aws-modules/terraform-aws-vpc.git"
  vcs_id          = data.terrakube_vcs.vcs.id
}

# Private repository through an SSH key.
resource "terrakube_module" "vpc_private_ssh" {
  name            = "vpc_private_ssh"
  organization_id = data.terrakube_organization.org.id
  description     = "cloudposse module"
  provider_name   = "aws"
  source          = "https://github.com/terraform-aws-modules/terraform-aws-vpc.git"
  ssh_id          = data.terrakube_ssh.ssh.id
}
