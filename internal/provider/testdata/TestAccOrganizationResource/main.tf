variable "name" {
  type = string
}

variable "description" {
  type = string
}

variable "execution_mode" {
  type = string
}

resource "terrakube_organization" "test" {
  name           = var.name
  description    = var.description
  execution_mode = var.execution_mode
}
