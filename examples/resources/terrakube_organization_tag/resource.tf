data "terrakube_organization" "org" {
  name = "my-organization"
}

resource "terrakube_organization_tag" "example" {
  name            = "example"
  organization_id = data.terrakube_organization.org.id
}
