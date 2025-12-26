resource "terrakube_collection_reference" "sample1" {
  organization_id = data.terrakube_organization.org.id
  collection_id   = terrakube_collection.sample1.id
  workspace_id    = terrakube_workspace_cli.sample1.id
  description     = "sample description"
}
