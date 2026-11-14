data "terrakube_organization" "org" {
  name = "my-organization"
}

resource "terrakube_collection" "collection" {
  name            = "TERRAKUBE_SUPER_COLLECTION"
  organization_id = data.terrakube_organization.org.id
  description     = "Hello World!"
  priority        = 10
}
