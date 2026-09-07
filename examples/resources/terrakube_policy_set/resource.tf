resource "terrakube_policy_set" "blast_radius" {
  organization_id   = "00000000-0000-0000-0000-000000000000"
  name              = "enterprise-blast-radius-guardrails"
  description       = "Limits accidental destruction and excessive resource blast radius"
  enforcement_level = "soft_mandatory"
  override_team     = "secops-approvers"
  global            = true

  vcs_id     = "11111111-1111-1111-1111-111111111111"
  repository = "https://github.com/enterprise/terrakube-policies.git"
  branch     = "v2.1.0"
  folder     = "bundles/common/blast_radius"
}
