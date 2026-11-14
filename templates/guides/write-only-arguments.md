---
page_title: "Write-only arguments"
subcategory: ""
description: |-
  Send secrets to Terrakube without storing them in the Terraform state.
---

# Write-only arguments

A write-only argument is sent to Terrakube but never stored in the plan or the state. They need
Terraform 1.11 or later, or OpenTofu 1.11 or later.

| Resource | Write-only argument | Version argument |
|---|---|---|
| `terrakube_organization_variable` | `value_wo` | `value_wo_version` |
| `terrakube_workspace_variable` | `value_wo` | `value_wo_version` |
| `terrakube_collection_item` | `value_wo` | `value_wo_version` |
| `terrakube_ssh` | `private_key_wo` | `private_key_wo_version` |
| `terrakube_vcs` | `client_secret_wo`, `private_key_wo` | `client_secret_wo_version`, `private_key_wo_version` |
| `terrakube_organization_notification_configuration` | `signing_secret_wo` | `signing_secret_wo_version` |
| `terrakube_workspace_notification_configuration` | `signing_secret_wo` | `signing_secret_wo_version` |

## Example

Pair a write-only argument with an ephemeral value, so the secret does not reach the state on the
Terraform side either.

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

- **Changing the value does not update Terrakube.** Terraform cannot see a write-only value
  change, so bump the `_wo_version` argument to send a new one.
- **Variables must be sensitive.** `value_wo` on the variable resources requires
  `sensitive = true`.
- **Pick one.** On resources that also have the plain argument (`value`, `private_key`,
  `client_secret`, `signing_secret`), set either that or the write-only one, not both.
- **No drift detection.** Terrakube never returns these values, so a change made outside Terraform
  is not detected.
- **Import leaves them empty.** After an import, the next plan shows an update that sends the
  value again.
