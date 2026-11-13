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
ephemeral "random_password" "vcs_secret" {
  length = 32
}

resource "terrakube_vcs" "vcs_wo" {
  organization_id          = data.terrakube_organization.org.id
  name                     = "Github write-only"
  description              = "github connection with a write-only secret"
  vcs_type                 = "GITHUB"
  client_id                = "Iv1.1b9b7b1b1b1b1b1b"
  client_secret_wo         = ephemeral.random_password.vcs_secret.result
  client_secret_wo_version = 1
  endpoint                 = "https://github.com"
  api_url                  = "https://api.github.com"
}
