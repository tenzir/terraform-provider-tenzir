data "tenzir_people" "acme" {
  organization_id = data.tenzir_organization.acme.id
}

locals {
  alice = one([for p in data.tenzir_people.acme.people : p.user_id if p.email == "alice@example.com"])
}

resource "tenzir_role_assignment" "alice_soc_reader" {
  organization_id = data.tenzir_organization.acme.id
  user_id         = local.alice
  role_id         = tenzir_role.soc_reader.id
}
