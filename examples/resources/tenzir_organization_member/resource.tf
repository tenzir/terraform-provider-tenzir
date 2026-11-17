data "tenzir_user" "alice" {
  email = "alice@example.com"
}

resource "tenzir_organization_member" "alice" {
  organization_id = tenzir_organization.demo.id
  user_id         = data.tenzir_user.alice.id
  role_ids        = [tenzir_role.visitor.id, "org.member"]
}
