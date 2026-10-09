resource "terrakube_workspace_tag" "example" {
  tag_id          = terrakube_tag.example.id
  workspace_id    = terrakube_workspace.example.id
  organization_id = terrakube_organization.example.id
}

# Key/value tag, selectable from the Terraform CLI with:
#   cloud { workspaces { tags = { env = "dev" } } }
resource "terrakube_workspace_tag" "env" {
  tag_id          = terrakube_organization_tag.env.id
  value           = "dev"
  workspace_id    = terrakube_workspace.example.id
  organization_id = terrakube_organization.example.id
}
