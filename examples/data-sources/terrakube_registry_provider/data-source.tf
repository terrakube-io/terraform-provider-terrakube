data "terrakube_organization" "org" {
  name = "my-org"
}

data "terrakube_registry_provider" "random" {
  organization_id = data.terrakube_organization.org.id
  name            = "random"
}

output "random_provider_id" {
  value = data.terrakube_registry_provider.random.id
}
