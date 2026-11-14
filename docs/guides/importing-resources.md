---
page_title: "Importing existing resources"
subcategory: ""
description: |-
  Bring objects that already exist in Terrakube under Terraform management.
---

# Importing existing resources

Most resources can be imported with `terraform import` (or `tofu import`). The ID is made of the
parent IDs and the object ID, separated by commas, with the organization first:

```shell
# Project: organization_id,id
terraform import terrakube_project.example 00000000-0000-0000-0000-000000000000,00000000-0000-0000-0000-000000000000

# Workspace variable: organization_id,workspace_id,id
terraform import terrakube_workspace_variable.example 00000000-0000-0000-0000-000000000000,00000000-0000-0000-0000-000000000000,00000000-0000-0000-0000-000000000000
```

The exact format is different for each resource, so use the **Import** section of its page. A few
resources are scoped differently. For example, `terrakube_workspace_schedule` takes
`workspace_id,id`, and `terrakube_organization` and `terrakube_workspace_run_trigger` take only
the ID.

## Things to know

- **`terrakube_workspace_tag` cannot be imported.** Delete the tag and create it again.
- **Secrets are not imported.** Terrakube does not return secrets (variable values marked
  sensitive, private keys, client secrets, signing secrets), so they are null after an import.
  The next plan shows an update that sends them again.
- **Find the IDs** in the Terrakube UI, or with the data sources, for example
  `data.terrakube_organization.org.id`.
