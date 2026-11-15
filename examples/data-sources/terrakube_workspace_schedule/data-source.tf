data "terrakube_organization" "org" {
  name = "my-organization"
}

data "terrakube_workspace" "ws" {
  name         = "my-workspace"
  organization = data.terrakube_organization.org.name
}

data "terrakube_organization_template" "plan" {
  name            = "plan"
  organization_id = data.terrakube_organization.org.id
}

# The schedule that runs the plan template in this workspace.
data "terrakube_workspace_schedule" "plan" {
  workspace_id = data.terrakube_workspace.ws.id
  template_id  = data.terrakube_organization_template.plan.id
}

# When the workspace runs the same template on more than one schedule,
# set schedule to choose one.
data "terrakube_workspace_schedule" "weekend_plan" {
  workspace_id = data.terrakube_workspace.ws.id
  template_id  = data.terrakube_organization_template.plan.id
  schedule     = "0 0 8 ? * SAT,SUN"
}

output "plan_schedule" {
  value = data.terrakube_workspace_schedule.plan.schedule
}
