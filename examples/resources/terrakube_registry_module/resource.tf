# Public module imported via the conventional GitHub naming.
# `public_registry_ref` is a convenience: the provider converts
# `cloudposse/label/null` to https://github.com/cloudposse/terraform-null-label.git
resource "terrakube_registry_module" "label" {
  organization_id     = data.terrakube_organization.org.id
  name                = "label"
  provider_name       = "null"
  public_registry_ref = "cloudposse/label/null"
}

# Same module registered with an explicit Git source URL.
resource "terrakube_registry_module" "vpc" {
  organization_id = data.terrakube_organization.org.id
  name            = "vpc"
  provider_name   = "aws"
  description     = "VPC module"
  source          = "https://github.com/terraform-aws-modules/terraform-aws-vpc.git"
}

# Private module backed by a VCS connection.
resource "terrakube_registry_module" "vpc_private" {
  organization_id = data.terrakube_organization.org.id
  name            = "vpc_private"
  provider_name   = "aws"
  description     = "Private VPC module"
  source          = "https://github.com/my-org/terraform-aws-vpc.git"
  vcs_id          = data.terrakube_vcs.vcs.id
}

# Private mono-repository module pulled over SSH with a tag prefix.
resource "terrakube_registry_module" "shared_libs" {
  organization_id = data.terrakube_organization.org.id
  name            = "shared"
  provider_name   = "aws"
  source          = "git@github.com:my-org/terraform-monorepo.git"
  ssh_id          = data.terrakube_ssh.ssh.id
  folder          = "/modules/shared/"
  tag_prefix      = "shared/"
}
