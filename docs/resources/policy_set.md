---
page_title: "terrakube_policy_set Resource - terrakube"
subcategory: ""
description: |-
  Create and manage an OPA Policy Set in Terrakube. Policy sets define bundles of Open Policy Agent (Rego) policies and guardrails linked to a Git VCS repository.
---

# terrakube_policy_set (Resource)

Create and manage an OPA Policy Set in Terrakube. Policy sets define bundles of Open Policy Agent (Rego) policies and guardrails linked to a Git VCS repository, scoped globally or attached to specific workspaces, projects, or tags.

## Example Usage

```terraform
resource "terrakube_policy_set" "blast_radius" {
  organization_id   = "00000000-0000-0000-0000-000000000000"
  name              = "enterprise-blast-radius-guardrails"
  description       = "Limits accidental destruction and excessive resource blast radius"
  enforcement_level = "soft_mandatory"
  override_team     = "secops-approvers"
  global            = true

  vcs_id     = "11111111-1111-1111-1111-111111111111"
  repository = "https://github.com/enterprise/terrakube-policies.git"
  branch     = "v2.1.0"
  folder     = "bundles/common/blast_radius"
}
```

## Schema

### Required

- `name` (String) Policy set name, unique within the organization
- `organization_id` (String) Terrakube organization ID

### Optional

- `branch` (String) Git branch or SemVer release tag (e.g. `v2.1.0`). Default is `main`.
- `description` (String) Description of the policy set and guardrails enforced
- `enforcement_level` (String) Enforcement level: `hard_mandatory`, `soft_mandatory`, or `advisory`. Default is `hard_mandatory`.
- `folder` (String) Subfolder within repository where Rego bundles are stored. Default is `/`.
- `global` (Boolean) Whether this policy set applies globally to all workspaces in the organization. Default is `false`.
- `notification_configuration_id` (String) Notification configuration ID to alert on policy violations
- `override_team` (String) Team name authorized to override soft_mandatory violations in CLI and UI
- `repository` (String) Git repository URL containing the OPA Rego policies
- `shadow_enforcement_level` (String) Optional shadow enforcement level for dark-launching or staging policies
- `vcs_id` (String) VCS connection ID for private policy repositories

### Read-Only

- `id` (String) Policy Set ID (UUID)

## Import

Import is supported using the following syntax:

```shell
# Policy Set can be imported with organization_id,id or id
terraform import terrakube_policy_set.example 00000000-0000-0000-0000-000000000000,00000000-0000-0000-0000-000000000000
```
