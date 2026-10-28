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
# Write-only value (Terraform 1.11+ / OpenTofu 1.11+): sent to Terrakube but
# never stored in the plan or state. It must be sensitive. Bump
# value_wo_version to push a new value.
ephemeral "random_password" "api_token" {
  length = 32
}

resource "terrakube_organization_variable" "sample3" {
  organization_id  = data.terrakube_organization.org.id
  key              = "API_TOKEN"
  value_wo         = ephemeral.random_password.api_token.result
  value_wo_version = 1
  description      = "write-only secret"
  category         = "ENV"
  sensitive        = true
  hcl              = false
}
