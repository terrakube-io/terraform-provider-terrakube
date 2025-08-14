<a href="https://terraform.io">
    <img src=".github/tf.png" alt="Terraform logo" title="Terraform" align="left" height="50" />
</a>

# Terraform Provider for Terrakube  

The Terrakube Terraform Provider allows managing resources within your Terrakube installation.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.19

## Building The Provider

1. Clone the repository
2. Enter the repository directory
3. Build the provider using the Go `install` command:

```shell
go install .
go generate ./...
```

## Usage Example

```hcl
terraform {
  required_providers {
    terrakube = {
      source = "terrakube-io/terrakube"
    }
  }
}

provider "terrakube" {
  endpoint = "http://terrakube-api.minikube.net"
  token    = "(PERSONAL ACCESS TOKEN OR TEAM TOKEN)"
}

resource "terrakube_organization" "org_sample" {
  name           = "unique-org-name"
  description    = "my super description"
  execution_mode = "local"
}

resource "terrakube_organization_variable" "sample1" {
  organization_id = data.terrakube_organization.org.id
  key             = "sample-env-var"
  value           = "sample-var2"
  description     = "sample env var2213241243"
  category        = "ENV"
  sensitive       = false
  hcl             = false
}

resource "terrakube_organization_variable" "sample2" {
  organization_id = data.terrakube_organization.org.id
  key             = "sample-terra-var"
  value           = "sample-terraform234"
  description     = "sample env var222212341234"
  category        = "TERRAFORM"
  sensitive       = false
  hcl             = false
}

data "terrakube_organization" "org" {
  name = "simple"
}

data "terrakube_vcs" "vcs" {
  name            = "sample_vcs"
  organization_id = data.terrakube_organization.org.id
}

data "terrakube_ssh" "ssh" {
  name            = "sample_ssh"
  organization_id = data.terrakube_organization.org.id
}

resource "terrakube_team" "team" {
  name             = "TERRAKUBE_SUPER_ADMIN"
  organization_id  = data.terrakube_vcs.vcs.organization_id
  manage_workspace = false
  manage_module    = false
  manage_provider  = true
  manage_vcs       = true
  manage_template  = true
}

resource "terrakube_module" "module1" {
  name            = "module_public_connection"
  organization_id = data.terrakube_ssh.ssh.organization_id
  description     = "module_public_connection"
  provider_name   = "aws"
  source          = "https://github.com/terraform-aws-modules/terraform-aws-vpc.git"
}

resource "terrakube_module" "module2" {
  name            = "module_vcs_connection"
  organization_id = data.terrakube_ssh.ssh.organization_id
  description     = "module_vcs_connection"
  provider_name   = "aws"
  source          = "https://github.com/terraform-aws-modules/super_private.git"
  vcs_id          = data.terrakube_vcs.vcs.id
}

resource "terrakube_module" "module3" {
  name            = "module_ssh_connection"
  organization_id = data.terrakube_ssh.ssh.organization_id
  description     = "module_ssh_connection"
  provider_name   = "aws"
  source          = "https://github.com/terraform-aws-modules/super_private.git"
  ssh_id          = data.terrakube_ssh.ssh.id
}
```

* [Terrakube Docs](https://docs.terrakube.io/).
* [Terrakube API Docs](https://docs.terrakube.io/api/methods).
