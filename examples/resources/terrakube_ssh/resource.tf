resource "terrakube_ssh" "ssh" {
  name            = "github-key"
  organization_id = data.terrakube_organization.org.id
  description     = "ssh key to get modules from github"
  private_key     = file("~/.ssh/id_rsa")
  ssh_type        = "rsa"
}

# Write-only key (Terraform 1.11+ / OpenTofu 1.11+): sent to Terrakube but
# never stored in the plan or state. Bump private_key_wo_version to push a
# new key.
variable "ssh_private_key" {
  type      = string
  sensitive = true
  ephemeral = true
}

resource "terrakube_ssh" "ssh_wo" {
  name                   = "github-key-wo"
  organization_id        = data.terrakube_organization.org.id
  description            = "ssh key with a write-only private key"
  private_key_wo         = var.ssh_private_key
  private_key_wo_version = 1
  ssh_type               = "rsa"
}
