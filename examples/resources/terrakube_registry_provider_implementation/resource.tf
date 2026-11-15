data "terrakube_organization" "org" {
  name = "my-organization"
}

data "terrakube_registry_provider" "internal" {
  organization_id = data.terrakube_organization.org.id
  name            = "internal"
}

resource "terrakube_registry_provider_version" "current" {
  organization_id = data.terrakube_organization.org.id
  provider_id     = data.terrakube_registry_provider.internal.id
  version_number  = "1.2.3"
  protocols       = ["6.0"]
}

# The public GPG key that signed the SHA256SUMS file, ASCII armored.
variable "signing_key" {
  type = string
}

# One implementation for each operating system and architecture of the release.
resource "terrakube_registry_provider_implementation" "linux_amd64" {
  organization_id = data.terrakube_organization.org.id
  provider_id     = data.terrakube_registry_provider.internal.id
  version_id      = terrakube_registry_provider_version.current.id

  os           = "linux"
  arch         = "amd64"
  filename     = "terraform-provider-internal_1.2.3_linux_amd64.zip"
  download_url = "https://releases.example.com/internal/1.2.3/terraform-provider-internal_1.2.3_linux_amd64.zip"
  shasum       = "5d9c0ab18b6e3b7a7c2f1f0d8a3f2c8f3a2b1c0d9e8f7a6b5c4d3e2f1a0b9c8d"

  shasums_url           = "https://releases.example.com/internal/1.2.3/terraform-provider-internal_1.2.3_SHA256SUMS"
  shasums_signature_url = "https://releases.example.com/internal/1.2.3/terraform-provider-internal_1.2.3_SHA256SUMS.sig"
  key_id                = "34365D9472D7468F"
  ascii_armor           = var.signing_key
}
