variable "name" {
  type = string
}

variable "value" {
  type = string
}

variable "description" {
  type = string
}

resource "terrakube_organization" "test" {
  name           = var.name
  execution_mode = "local"
}

# The admin group can create organizations, but managing variables in one needs a team with
# manage_collection in that organization.
resource "terrakube_team" "admins" {
  organization_id   = terrakube_organization.test.id
  name              = "TERRAKUBE_ADMIN"
  manage_workspace  = true
  manage_collection = true
}

resource "terrakube_organization_variable" "test" {
  depends_on      = [terrakube_team.admins]
  organization_id = terrakube_organization.test.id
  key             = "ACC_VARIABLE"
  value           = var.value
  description     = var.description
  category        = "ENV"
  sensitive       = false
  hcl             = false
}

# Terrakube never returns the value of a sensitive variable, so the provider keeps the configured one.
resource "terrakube_organization_variable" "secret" {
  depends_on      = [terrakube_team.admins]
  organization_id = terrakube_organization.test.id
  key             = "ACC_SECRET"
  value           = var.value
  description     = var.description
  category        = "ENV"
  sensitive       = true
  hcl             = false
}
