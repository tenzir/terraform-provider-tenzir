resource "tenzir_role" "soc_reader" {
  organization_id = data.tenzir_organization.acme.id
  label           = "SOC reader"
  color           = "blue"
  description     = "Reads the SOC workspace."
  policy = jsonencode([
    [
      {
        target        = { _tag = "Workspace", orgId = data.tenzir_organization.acme.id, workspaceId = tenzir_workspace.soc.id }
        relationships = []
      },
      ["Workspace.read", "Workspace.readFileTree", "Workspace.readFile"],
    ],
  ])
}
