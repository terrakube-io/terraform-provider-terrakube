data "terrakube_organization" "org" {
  name = "my-organization"
}

resource "terrakube_self_hosted_agent" "collection" {
  name            = "MY-SUPER-AGENT"
  organization_id = data.terrakube_organization.org.id
  description     = "Hello World!"
  url             = "http://localhost:8090"
}
