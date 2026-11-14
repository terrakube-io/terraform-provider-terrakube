data "terrakube_organization" "org" {
  name = "my-organization"
}

data "terrakube_workspace" "ws" {
  name         = "my-workspace"
  organization = data.terrakube_organization.org.name
}

# category is only needed when the key exists as both ENV and TERRAFORM.
data "terrakube_workspace_variable" "instance_type" {
  organization_id = data.terrakube_organization.org.id
  workspace_id    = data.terrakube_workspace.ws.id
  key             = "instance_type"
  category        = "TERRAFORM"
}

output "instance_type" {
  value     = data.terrakube_workspace_variable.instance_type.value
  sensitive = true
}
