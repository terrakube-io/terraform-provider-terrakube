terraform {
  required_providers {
    terrakube = {
      source = "terrakube-io/terrakube"
    }
  }
}

provider "terrakube" {
  endpoint             = "http://terrakube-api.minikube.net"
  token                = "12345"
  insecure_http_client = true
}
