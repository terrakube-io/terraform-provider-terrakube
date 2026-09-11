# Plan-only run on pull requests, triggered only when files under terraform/
# or modules/ change. Paths are matched as simple wildcard patterns.
resource "terrakube_workspace_webhook_event" "pull_request" {
  webhook_id  = terrakube_workspace_webhook_v2.main.id
  event       = "PULL_REQUEST"
  branch      = ["^(?!main$).*$"]
  path        = ["terraform/*", "modules/**"]
  path_type   = "PATTERN"
  priority    = 100
  template_id = data.terrakube_organization_template.plan.id

  pr_workflow_enabled = true
  pr_apply_enabled    = false
}

# Plan and apply on pushes to main. path_type defaults to REGEX, so each path
# entry is a full regular expression matched against changed files.
resource "terrakube_workspace_webhook_event" "push_main" {
  webhook_id  = terrakube_workspace_webhook_v2.main.id
  event       = "PUSH"
  branch      = ["main"]
  path        = ["^terraform/.*\\.tf$"]
  priority    = 100
  template_id = data.terrakube_organization_template.plan_and_apply.id
}
