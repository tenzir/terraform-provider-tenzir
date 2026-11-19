resource "tenzir_deployment" "edge" {
  organization_id = data.tenzir_organization.acme.id
  workspace_id    = tenzir_workspace.soc.id
  name            = "Edge collector"
  branch          = "main"
  auto_deploy     = true
}
