data "terrakube_organization" "org" {
  name = "my-organization"
}

data "terrakube_collection" "shared" {
  organization_id = data.terrakube_organization.org.id
  name            = "shared-settings"
}

data "terrakube_workspace" "ws" {
  name         = "my-workspace"
  organization = data.terrakube_organization.org.name
}

resource "terrakube_collection_reference" "sample1" {
  organization_id = data.terrakube_organization.org.id
  collection_id   = data.terrakube_collection.shared.id
  workspace_id    = data.terrakube_workspace.ws.id
  description     = "sample description"
}
