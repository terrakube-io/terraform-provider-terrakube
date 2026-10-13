resource "terrakube_collection_item" "sample1" {
  organization_id = data.terrakube_organization.org.id
  collection_id   = terrakube_collection.sample1.id
  key             = "sample-env-var"
  value           = "sample-value2222"
  description     = "sample env var"
  category        = "ENV"
  sensitive       = false
  hcl             = false
}

resource "terrakube_collection_item" "sample2" {
  organization_id = data.terrakube_organization.org.id
  collection_id   = terrakube_collection.sample1.id
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
ephemeral "random_password" "api_token" {
  length = 32
}

resource "terrakube_collection_item" "sample3" {
  organization_id  = data.terrakube_organization.org.id
  collection_id    = terrakube_collection.sample1.id
  key              = "API_TOKEN"
  value_wo         = ephemeral.random_password.api_token.result
  value_wo_version = 1
  description      = "write-only secret"
  category         = "ENV"
  sensitive        = true
  hcl              = false
}
