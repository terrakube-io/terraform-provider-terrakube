# Example 1: Workspace-scoped attachment
resource "terrakube_policy_attachment" "workspace_attachment" {
  policy_set_id = terrakube_policy_set.blast_radius.id
  workspace_id  = "22222222-2222-2222-2222-222222222222"
}

# Example 2: Project-scoped attachment (applies to all workspaces in project)
resource "terrakube_policy_attachment" "project_attachment" {
  policy_set_id = terrakube_policy_set.blast_radius.id
  project_id    = "33333333-3333-3333-3333-333333333333"
}

# Example 3: Tag-scoped attachment (applies to workspaces bearing tag)
resource "terrakube_policy_attachment" "tag_attachment" {
  policy_set_id = terrakube_policy_set.blast_radius.id
  tag_id        = "44444444-4444-4444-4444-444444444444"
}
