data "tenzir_deployment" "edge" {
  id = tenzir_deployment.edge.id
}

output "edge_connected" {
  value = data.tenzir_deployment.edge.reachable
}
