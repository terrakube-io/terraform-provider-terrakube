data "terrakube_organization" "org" {
  name = "my-organization"
}

data "terrakube_workspace" "upstream" {
  name         = "upstream"
  organization = data.terrakube_organization.org.name
}

data "terrakube_workspace" "downstream" {
  name         = "downstream"
  organization = data.terrakube_organization.org.name
}

data "terrakube_organization_template" "plan_apply" {
  name            = "plan-and-apply"
  organization_id = data.terrakube_organization.org.id
}

# A successful apply in upstream queues a run in downstream.
resource "terrakube_workspace_run_trigger" "example" {
  source_workspace_id      = data.terrakube_workspace.upstream.id
  destination_workspace_id = data.terrakube_workspace.downstream.id
  template_id              = data.terrakube_organization_template.plan_apply.id
  enabled                  = true
}
