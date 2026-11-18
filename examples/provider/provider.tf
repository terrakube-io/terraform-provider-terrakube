terraform {
  required_providers {
    terrakube = {
      source = "terrakube-io/terrakube"
    }
  }
}

# Reads the API endpoint and token from the TERRAKUBE_ENDPOINT and
# TERRAKUBE_TOKEN environment variables.
provider "terrakube" {}
