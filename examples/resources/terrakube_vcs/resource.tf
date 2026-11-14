data "terrakube_organization" "org" {
  name = "my-organization"
}

resource "terrakube_vcs" "vcs" {
  organization_id = data.terrakube_organization.org.id
  name            = "Github"
  description     = "test github connection"
  vcs_type        = "GITHUB"
  client_id       = "Iv1.1b9b7b1b1b1b1b1b"
  client_secret   = "hellotest"
  endpoint        = "https://github.com"
  api_url         = "https://api.github.com"
}

# Write-only secret (Terraform 1.11+ / OpenTofu 1.11+): sent to Terrakube but
# never stored in the plan or state. Bump client_secret_wo_version to push a
# new secret. private_key_wo / private_key_wo_version work the same way.
variable "vcs_client_secret" {
  type      = string
  sensitive = true
  ephemeral = true
}

resource "terrakube_vcs" "vcs_wo" {
  organization_id          = data.terrakube_organization.org.id
  name                     = "Github write-only"
  description              = "github connection with a write-only secret"
  vcs_type                 = "GITHUB"
  client_id                = "Iv1.1b9b7b1b1b1b1b1b"
  client_secret_wo         = var.vcs_client_secret
  client_secret_wo_version = 1
  endpoint                 = "https://github.com"
  api_url                  = "https://api.github.com"
}
