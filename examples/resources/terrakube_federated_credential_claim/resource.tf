resource "terrakube_federated_credential" "github_actions" {
  name       = "GitHub Actions"
  issuer_url = "https://token.actions.githubusercontent.com"
  audience   = "terrakube-audience"
}

resource "terrakube_federated_credential_claim" "repository_owner" {
  federated_credential_id = terrakube_federated_credential.github_actions.id
  claim_key               = "repository_owner"
  claim_value             = "terrakube-org"
}
