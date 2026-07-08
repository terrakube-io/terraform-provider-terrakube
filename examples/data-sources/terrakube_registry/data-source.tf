data "terrakube_organization" "org" {
  name = "my-org"
}

# List every module and provider published in the organization's registry.
data "terrakube_registry" "all" {
  organization_id = data.terrakube_organization.org.id
}

output "module_count" {
  value = length(data.terrakube_registry.all.modules)
}

output "provider_count" {
  value = length(data.terrakube_registry.all.providers)
}
