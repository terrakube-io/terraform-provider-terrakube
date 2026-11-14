data "terrakube_organization" "org" {
  name = "my-organization"
}

data "terrakube_collection" "shared" {
  organization_id = data.terrakube_organization.org.id
  name            = "shared-settings"
}

output "collection_priority" {
  value = data.terrakube_collection.shared.priority
}
