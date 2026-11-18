variable "name" {
  type = string
}

variable "value" {
  type      = string
  sensitive = true
}

variable "value_version" {
  type = number
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

# value_wo is never stored in state. Terrakube only gets a new value when value_wo_version changes.
resource "terrakube_organization_variable" "test" {
  depends_on       = [terrakube_team.admins]
  organization_id  = terrakube_organization.test.id
  key              = "ACC_WRITE_ONLY"
  value_wo         = var.value
  value_wo_version = var.value_version
  description      = "Acceptance test write-only variable"
  category         = "ENV"
  sensitive        = true
  hcl              = false
}
