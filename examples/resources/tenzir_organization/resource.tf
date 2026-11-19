# A platform administrator sets up an organization on the provisioned plan.
# It starts without members: see tenzir_organization_member for making the
# managing account its owner.
resource "tenzir_organization" "demo" {
  slug = "demo"
  name = "Demo"
  plan = "provisioned"
}

# Omit the plan to create a Community organization with the authenticated
# user as its sole owner (User.createOrganization).
resource "tenzir_organization" "acme" {
  slug = "acme"
}
