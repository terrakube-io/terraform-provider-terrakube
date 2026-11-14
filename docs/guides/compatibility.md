---
page_title: "Supported Terrakube versions"
subcategory: ""
description: |-
  Which Terrakube server versions the provider is built for, and which features need a newer server.
---

# Supported Terrakube versions

Terrakube can change its API in any release, and the provider is released separately from the
server. Use a provider version that matches your server, and upgrade both together.

| Provider version | Terrakube server |
|---|---|
| 0.30.0 and later | 2.34.0 |
| Before 0.30.0 | Not recorded per release |

If you run an older server, a feature listed below may not work. Please
[open an issue](https://github.com/terrakube-io/terraform-provider-terrakube/issues/new/choose)
with the provider and Terrakube versions if you hit a problem on a combination that is not listed.

## Features that need a newer server

| Feature | Minimum Terrakube |
|---|---|
| `terrakube_workspace_run_trigger` | 2.34.0 |
| `destroy_job` on `terrakube_team`, `terrakube_project_access` and `terrakube_workspace_access` | 2.34.0 |
| `require_approve_to_apply` and `restrict_apply_to_configured_branch` on `terrakube_workspace_cli` and `terrakube_workspace_vcs` | 2.34.0 |
| `path_type` on `terrakube_workspace_webhook_event` | 2.31.0 |
| `terrakube_workspace_webhook_v2` and `terrakube_workspace_webhook_event` | 2.26.0 |

Attributes that older servers do not know are optional, and are left out of the request when you
do not set them. A configuration that does not use them keeps working on an older server.
