data "terrakube_organization" "org" {
  name = "my-organization"
}

resource "terrakube_organization_template" "example" {
  name            = "example"
  organization_id = data.terrakube_organization.org.id
  description     = "Example organization template"
  version         = "1.0.0"
  content         = <<EOF
flow:
  - type: "terraformPlan"
    name: "Plan"
    step: 100
EOF
}
