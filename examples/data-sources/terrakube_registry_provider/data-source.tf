data "terrakube_organization" "org" {
  name = "my-organization"
}

data "terrakube_registry_provider" "aws" {
  organization_id = data.terrakube_organization.org.id
  name            = "aws"
}

output "aws_is_mirrored" {
  value = data.terrakube_registry_provider.aws.imported
}
