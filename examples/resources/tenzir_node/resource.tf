resource "tenzir_node" "example" {
  workspace_id = tenzir_workspace.example.id
  name         = "ingest-node-01"
}
