---
page_title: "Importing existing resources"
subcategory: ""
description: |-
  Bring objects that already exist in Terrakube under Terraform management.
---

# Importing existing resources

Most resources can be imported with `terraform import` (or `tofu import`). The ID is made of the
parent IDs and the object ID, separated by commas, organization first:

```shell
# Project: organization_id,id
terraform import terrakube_project.example 00000000-0000-0000-0000-000000000000,00000000-0000-0000-0000-000000000000
```

The format differs per resource, so use the **Import** section of its page.

- `terrakube_workspace_tag` cannot be imported. Delete the tag and create it again.
- Terrakube does not return secrets, so they are empty after an import and the next plan shows an
  update that sends them again.
