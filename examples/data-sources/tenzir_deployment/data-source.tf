data "tenzir_deployment" "edge" {
  organization_id = tenzir_deployment.edge.organization_id
  workspace_id    = tenzir_deployment.edge.workspace_id
  id              = tenzir_deployment.edge.id
}

output "edge_connected" {
  value = data.tenzir_deployment.edge.reachable
}
