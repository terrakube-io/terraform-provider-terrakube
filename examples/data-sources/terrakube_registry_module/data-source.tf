data "terrakube_organization" "org" {
  name = "my-org"
}

data "terrakube_registry_module" "label" {
  organization_id = data.terrakube_organization.org.id
  name            = "label"
  provider_name   = "null"
}

output "label_source" {
  value = data.terrakube_registry_module.label.source
}
