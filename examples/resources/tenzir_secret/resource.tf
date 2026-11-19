variable "splunk_token" {
  type      = string
  sensitive = true
  ephemeral = true
}

# Every deployment of the organization may read this one.
resource "tenzir_secret" "splunk" {
  organization_id = data.tenzir_organization.acme.id
  name            = "splunk_token"
  value           = var.splunk_token
  value_version   = 1
}

# Only the deployments of the SOC workspace see this one.
resource "tenzir_secret" "soc_splunk" {
  organization_id = data.tenzir_organization.acme.id
  workspace_id    = tenzir_workspace.soc.id
  name            = "splunk_token"
  value           = var.splunk_token
  value_version   = 1
}
