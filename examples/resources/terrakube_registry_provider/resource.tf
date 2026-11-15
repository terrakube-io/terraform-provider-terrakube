data "terrakube_organization" "org" {
  name = "my-organization"
}

# Mirror a provider from the public registry (Terrakube 2.30.0+). Terrakube keeps
# its versions up to date.
resource "terrakube_registry_provider" "aws" {
  organization_id    = data.terrakube_organization.org.id
  name               = "aws"
  description        = "AWS provider mirrored from the public registry"
  imported           = true
  registry_namespace = "hashicorp"
}

# A provider you build and host yourself.
resource "terrakube_registry_provider" "internal" {
  organization_id = data.terrakube_organization.org.id
  name            = "internal"
  description     = "Provider for our internal platform"
}
