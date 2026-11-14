data "terrakube_organization" "org" {
  name = "simple"
}

data "terrakube_organization_tag" "tag" {
  organization_id = data.terrakube_organization.org.id
  name            = "test"
}
