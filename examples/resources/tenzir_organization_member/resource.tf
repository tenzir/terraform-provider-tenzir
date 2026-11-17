# The account that Terraform runs as manages the organization, so it becomes
# the owner of the organization it set up.
resource "tenzir_organization" "demo" {
  slug = "demo"
  plan = "provisioned"
}

data "tenzir_current_user" "terraform" {}

resource "tenzir_organization_member" "terraform" {
  organization_id = tenzir_organization.demo.id
  user_id         = data.tenzir_current_user.terraform.id
  role_ids        = ["org.owner"]
}

# The platform refuses a change to the organization's roles while nobody can
# restore access to it, so roles wait for the owner.
resource "tenzir_role" "visitor" {
  organization_id = tenzir_organization.demo.id
  label           = "Visitor"
  color           = "neutral"
  policy = jsonencode([
    [{ target = { _tag = "Organization", orgId = tenzir_organization.demo.id }, relationships = [] }, ["Organization.read"]],
  ])

  depends_on = [tenzir_organization_member.terraform]
}
