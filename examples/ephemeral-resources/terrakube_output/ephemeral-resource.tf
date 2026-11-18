# Read the outputs of another workspace without storing them in plan or state.
ephemeral "terrakube_output" "network" {
  organization = "acme"
  workspace    = "network"
}

variable "organization_id" {
  type = string
}

variable "workspace_id" {
  type = string
}

# Pass a sensitive output straight to a write-only argument.
resource "terrakube_workspace_variable" "db_password" {
  organization_id  = var.organization_id
  workspace_id     = var.workspace_id
  key              = "db_password"
  value_wo         = ephemeral.terrakube_output.network.values.db_password
  value_wo_version = 1
  description      = "Database password read from the network workspace"
  category         = "TERRAFORM"
  sensitive        = true
  hcl              = false
}
