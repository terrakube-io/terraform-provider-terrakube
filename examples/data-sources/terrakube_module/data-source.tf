data "terrakube_organization" "org" {
  name = "my-organization"
}

data "terrakube_module" "vpc" {
  organization_id = data.terrakube_organization.org.id
  name            = "vpc"
  provider_name   = "aws"
}

output "module_source" {
  value = data.terrakube_module.vpc.source
}
