data "tenzir_people" "acme" {
  organization_id = data.tenzir_organization.acme.id
}

output "admins" {
  value = [for p in data.tenzir_people.acme.people : p.email if contains(p.role_ids, "org.admin")]
}
