# Tenzir-internal. Terraform runs as a platform administrator: it sets the
# organization up, makes its own user the owner, gives visitors a role, and
# then makes the organization the one every new user joins.
resource "tenzir_organization" "demo" {
  slug = "demo"
  name = "Demo"
  plan = "provisioned"
}

data "tenzir_current_user" "terraform" {}

resource "tenzir_organization_member" "terraform" {
  organization_id = tenzir_organization.demo.id
  user_id         = data.tenzir_current_user.terraform.id
  role_ids        = ["org.owner"]
}

resource "tenzir_role" "visitor" {
  organization_id = tenzir_organization.demo.id
  label           = "Visitor"
  color           = "neutral"
  policy = jsonencode([
    [{ target = { _tag = "Organization", orgId = tenzir_organization.demo.id }, relationships = [] }, ["Organization.read", "Organization.listWorkspaces"]],
  ])

  # The platform refuses role changes while nobody can restore access.
  depends_on = [tenzir_organization_member.terraform]
}

resource "tenzir_demo_organization" "this" {
  organization_id = tenzir_organization.demo.id
  role_id         = tenzir_role.visitor.id
}
