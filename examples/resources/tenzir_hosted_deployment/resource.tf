data "tenzir_deployment_types" "soc" {
  organization_id = data.tenzir_organization.acme.id
  workspace_id    = tenzir_workspace.soc.id
}

resource "tenzir_hosted_deployment" "cloud" {
  organization_id = data.tenzir_organization.acme.id
  workspace_id    = tenzir_workspace.soc.id
  name            = "Cloud"
  size            = data.tenzir_deployment_types.soc.default_size
  region          = data.tenzir_deployment_types.soc.regions[0]

  timeouts {
    create = "20m"
  }
}
