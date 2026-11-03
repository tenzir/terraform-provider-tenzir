data "tenzir_roles" "acme" {
  organization_id = data.tenzir_organization.acme.id
}

output "custom_roles" {
  value = [for r in data.tenzir_roles.acme.roles : r.label if !r.builtin]
}
