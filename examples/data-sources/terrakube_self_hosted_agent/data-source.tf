data "terrakube_organization" "org" {
  name = "simple"
}

data "terrakube_self_hosted_agent" "agent" {
  organization_id = data.terrakube_organization.org.id
  name            = "MY-SUPER-AGENT"
}
