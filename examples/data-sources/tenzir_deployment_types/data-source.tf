data "tenzir_deployment_types" "soc" {
  organization_id = data.tenzir_organization.acme.id
  workspace_id    = tenzir_workspace.soc.id
}

output "hosted_sizes" {
  value = [for t in data.tenzir_deployment_types.soc.types : t.size]
}
