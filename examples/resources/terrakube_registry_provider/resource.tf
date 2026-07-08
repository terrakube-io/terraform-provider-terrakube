# Mirror the public `hashicorp/random` provider into the organization's private registry.
# Terrakube will schedule a refresh job to fetch versions and platform implementations
# from registry.terraform.io.
resource "terrakube_registry_provider" "random" {
  organization_id    = data.terrakube_organization.org.id
  name               = "random"
  description        = "Mirror of hashicorp/random"
  imported           = true
  registry_namespace = "hashicorp"
}

# A fully-private provider. Versions and platform implementations are uploaded
# out-of-band (the Terrakube version/implementation endpoints).
resource "terrakube_registry_provider" "internal" {
  organization_id = data.terrakube_organization.org.id
  name            = "internal-tooling"
  description     = "Custom internal provider"
}
