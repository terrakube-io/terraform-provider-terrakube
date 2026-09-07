---
page_title: "terrakube_policy_attachment Resource - terrakube"
subcategory: ""
description: |-
  Attach a Terrakube Policy Set to a workspace, project, or tag scope.
---

# terrakube_policy_attachment (Resource)

Attach a Terrakube Policy Set to a workspace, project, or tag scope. Workspaces evaluate the cumulative union of all policy sets attached globally, to their parent project, to their assigned tags, and directly to the workspace.

## Example Usage

```terraform
# Attach to a specific workspace
resource "terrakube_policy_attachment" "workspace_attachment" {
  policy_set_id = terrakube_policy_set.blast_radius.id
  workspace_id  = "22222222-2222-2222-2222-222222222222"
}

# Attach to a project (all workspaces in the project)
resource "terrakube_policy_attachment" "project_attachment" {
  policy_set_id = terrakube_policy_set.blast_radius.id
  project_id    = "33333333-3333-3333-3333-333333333333"
}

# Attach to an organization tag (all workspaces bearing the tag)
resource "terrakube_policy_attachment" "tag_attachment" {
  policy_set_id = terrakube_policy_set.blast_radius.id
  tag_id        = "44444444-4444-4444-4444-444444444444"
}
```

## Schema

### Required

- `policy_set_id` (String) ID of the target Policy Set

### Optional

- `project_id` (String) Project ID to attach the policy set to. Mutually exclusive with `workspace_id` and `tag_id`.
- `tag_id` (String) Organization Tag ID to attach the policy set to. Mutually exclusive with `workspace_id` and `project_id`.
- `workspace_id` (String) Workspace ID to attach the policy set to. Mutually exclusive with `project_id` and `tag_id`.

### Read-Only

- `id` (String) Attachment ID (UUID)

## Import

Import is supported using the following syntax:

```shell
# Policy Attachment can be imported with policy_set_id,id
terraform import terrakube_policy_attachment.example 00000000-0000-0000-0000-000000000000,11111111-1111-1111-1111-111111111111
```
