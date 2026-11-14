<a href="https://terraform.io">
    <img src=".github/tf.png" alt="Terraform logo" title="Terraform" align="left" height="50" />
</a>

# Terraform Provider for Terrakube

Manage a [Terrakube](https://docs.terrakube.io/) installation with Terraform or OpenTofu:
organizations, workspaces, variables, teams, VCS and SSH connections, modules, policies, and more.

- [Provider documentation](https://registry.terraform.io/providers/terrakube-io/terrakube/latest/docs)
- [Terrakube documentation](https://docs.terrakube.io/)

## Requirements

- Terraform 1.0 or later, or OpenTofu. Write-only arguments (`*_wo`) need 1.11 or later of either.
- A Terrakube API and an access token. See [API tokens](https://docs.terrakube.io/user-guide/organizations/api-tokens).

## Usage

```hcl
terraform {
  required_providers {
    terrakube = {
      source = "terrakube-io/terrakube"
    }
  }
}

provider "terrakube" {
  endpoint = "https://terrakube-api.example.com"
  token    = var.terrakube_token
}

data "terrakube_organization" "org" {
  name = "my-organization"
}

resource "terrakube_project" "project" {
  name            = "platform"
  organization_id = data.terrakube_organization.org.id
  description     = "Groups related workspaces together"
}
```

`endpoint` and `token` can also come from the `TERRAKUBE_ENDPOINT` and `TERRAKUBE_TOKEN`
environment variables. Set `insecure_http_client = true` to skip certificate validation for a
development instance.

There is an example for every resource and data source in [examples/](examples/).

## Guides

- [Supported Terrakube versions](https://registry.terraform.io/providers/terrakube-io/terrakube/latest/docs/guides/compatibility)
- [Write-only arguments](https://registry.terraform.io/providers/terrakube-io/terrakube/latest/docs/guides/write-only-arguments)
- [Importing existing resources](https://registry.terraform.io/providers/terrakube-io/terrakube/latest/docs/guides/importing-resources)

## Terrakube versions

Terrakube can change its API in any release, and this provider is released separately. Releases
from 0.30.0 are built for Terrakube 2.34.0. A few resources and attributes need a newer server
than others. The guide above lists them.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for building, testing and the pull request format, and
[RELEASING.md](RELEASING.md) for how releases are made.
