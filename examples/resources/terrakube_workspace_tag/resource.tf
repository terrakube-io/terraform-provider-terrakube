data "terrakube_organization" "org" {
  name = "my-organization"
}

data "terrakube_workspace" "ws" {
  name         = "my-workspace"
  organization = data.terrakube_organization.org.name
}

data "terrakube_organization_tag" "example" {
  organization_id = data.terrakube_organization.org.id
  name            = "example"
}

data "terrakube_organization_tag" "env" {
  organization_id = data.terrakube_organization.org.id
  name            = "env"
}

resource "terrakube_workspace_tag" "example" {
  tag_id          = data.terrakube_organization_tag.example.id
  workspace_id    = data.terrakube_workspace.ws.id
  organization_id = data.terrakube_organization.org.id
}

# Key/value tag, selectable from the Terraform CLI with:
#   cloud { workspaces { tags = { env = "dev" } } }
resource "terrakube_workspace_tag" "env" {
  tag_id          = data.terrakube_organization_tag.env.id
  value           = "dev"
  workspace_id    = data.terrakube_workspace.ws.id
  organization_id = data.terrakube_organization.org.id
}
