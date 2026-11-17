data "tenzir_members" "acme" {
  organization_id = data.tenzir_organization.acme.id
}

locals {
  alice = one([for m in data.tenzir_members.acme.members : m.id if m.email == "alice@example.com"])
}

resource "tenzir_role_assignment" "alice_soc_reader" {
  organization_id = data.tenzir_organization.acme.id
  member_id       = local.alice
  role_id         = tenzir_role.soc_reader.id
}
