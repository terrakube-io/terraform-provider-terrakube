---
page_title: "Write-only arguments"
subcategory: ""
description: |-
  Send secrets to Terrakube without storing them in the Terraform state.
---

# Write-only arguments

An argument ending in `_wo` is write-only: it is sent to Terrakube but never stored in the plan or
the state. It needs Terraform 1.11 or later, or OpenTofu 1.11 or later. Each one is marked
**Write-only** in the documentation of its resource, with a `_wo_version` argument next to it.

Pair it with an ephemeral value, so the secret does not reach the state on the Terraform side
either:

```terraform
ephemeral "random_password" "api_token" {
  length = 32
}

resource "terrakube_workspace_variable" "api_token" {
  organization_id  = data.terrakube_organization.org.id
  workspace_id     = data.terrakube_workspace.ws.id
  key              = "API_TOKEN"
  value_wo         = ephemeral.random_password.api_token.result
  value_wo_version = 1
  description      = "write-only secret"
  category         = "ENV"
  sensitive        = true
  hcl              = false
}
```

## Things to know

- Terraform cannot see a write-only value change. Bump `_wo_version` to send a new one.
- Where a resource also has the plain argument, set one or the other, not both.
- Variables need `sensitive = true` to use `value_wo`.
- Terrakube never returns these values, so a change made outside Terraform is not detected, and
  an import leaves them empty.
