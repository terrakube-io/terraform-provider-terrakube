data "terrakube_organization" "org" {
  name = "my-organization"
}

data "terrakube_registry_provider" "internal" {
  organization_id = data.terrakube_organization.org.id
  name            = "internal"
}

resource "terrakube_registry_provider_version" "current" {
  organization_id = data.terrakube_organization.org.id
  provider_id     = data.terrakube_registry_provider.internal.id
  version_number  = "1.2.3"
  protocols       = ["6.0"]
}

# Still served, with a warning (Terrakube 2.34.0+).
resource "terrakube_registry_provider_version" "old" {
  organization_id     = data.terrakube_organization.org.id
  provider_id         = data.terrakube_registry_provider.internal.id
  version_number      = "1.0.0"
  protocols           = ["5.0"]
  status              = "deprecated"
  deprecation_message = "Use 1.2.3 or later"
}
