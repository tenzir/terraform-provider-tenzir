data "tenzir_members" "acme" {
  organization_id = data.tenzir_organization.acme.id
}

output "owners" {
  value = [for m in data.tenzir_members.acme.members : m.email if contains(m.role_ids, "org.owner")]
}
