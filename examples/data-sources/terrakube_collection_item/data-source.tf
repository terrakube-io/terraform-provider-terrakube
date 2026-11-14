data "terrakube_organization" "org" {
  name = "my-organization"
}

data "terrakube_collection" "shared" {
  organization_id = data.terrakube_organization.org.id
  name            = "shared-settings"
}

# category is only needed when the key exists as both ENV and TERRAFORM.
data "terrakube_collection_item" "region" {
  organization_id = data.terrakube_organization.org.id
  collection_id   = data.terrakube_collection.shared.id
  key             = "AWS_REGION"
  category        = "ENV"
}

output "aws_region" {
  value     = data.terrakube_collection_item.region.value
  sensitive = true
}
