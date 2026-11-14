data "terrakube_organization" "org" {
  name = "my-organization"
}

data "terrakube_collection" "shared" {
  organization_id = data.terrakube_organization.org.id
  name            = "shared-settings"
}

resource "terrakube_collection_item" "sample1" {
  organization_id = data.terrakube_organization.org.id
  collection_id   = data.terrakube_collection.shared.id
  key             = "sample-env-var"
  value           = "sample-value2222"
  description     = "sample env var"
  category        = "ENV"
  sensitive       = false
  hcl             = false
}

resource "terrakube_collection_item" "sample2" {
  organization_id = data.terrakube_organization.org.id
  collection_id   = data.terrakube_collection.shared.id
  key             = "sample-terra-var"
  value           = "sample-TERRAFORM"
  description     = "sample env var"
  category        = "TERRAFORM"
  sensitive       = false
  hcl             = false
}

# Write-only value (Terraform 1.11+ / OpenTofu 1.11+): sent to Terrakube but
# never stored in the plan or state. It must be sensitive. Bump
# value_wo_version to push a new value.
variable "api_token" {
  type      = string
  sensitive = true
  ephemeral = true
}

resource "terrakube_collection_item" "sample3" {
  organization_id  = data.terrakube_organization.org.id
  collection_id    = data.terrakube_collection.shared.id
  key              = "API_TOKEN"
  value_wo         = var.api_token
  value_wo_version = 1
  description      = "write-only secret"
  category         = "ENV"
  sensitive        = true
  hcl              = false
}
