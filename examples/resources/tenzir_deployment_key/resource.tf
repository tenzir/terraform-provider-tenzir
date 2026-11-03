resource "tenzir_deployment_key" "edge" {
  deployment_id = tenzir_deployment.edge.id
  # Change this to mint a new key.
  triggers = {
    rotation = "2026-11"
  }
}

# The deployment reads its key from the file that `tenzir-up --key-file` names.
resource "local_sensitive_file" "edge_key" {
  filename = "${path.module}/edge.key"
  content  = tenzir_deployment_key.edge.key
}
