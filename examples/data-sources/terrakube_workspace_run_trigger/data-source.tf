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

# The run trigger that queues a run in downstream after an apply in upstream.
data "terrakube_workspace_run_trigger" "upstream_to_downstream" {
  source_workspace_id      = data.terrakube_workspace.upstream.id
  destination_workspace_id = data.terrakube_workspace.downstream.id
}

output "run_trigger_enabled" {
  value = data.terrakube_workspace_run_trigger.upstream_to_downstream.enabled
}
