resource "terrakube_federated_credential" "github_actions" {
  name       = "GitHub Actions"
  issuer_url = "https://token.actions.githubusercontent.com"
  audience   = "terrakube-audience"
}
