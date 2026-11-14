data "terrakube_organization" "org" {
  name = "my-organization"
}

data "terrakube_organization_variable" "region" {
  organization_id = data.terrakube_organization.org.id
  key             = "AWS_REGION"
  category        = "ENV"
}

output "aws_region" {
  value     = data.terrakube_organization_variable.region.value
  sensitive = true
}
