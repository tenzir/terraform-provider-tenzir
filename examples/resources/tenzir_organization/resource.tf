# A platform administrator sets up an organization on the provisioned plan.
# It starts without members: see tenzir_organization_member for making the
# managing account its owner.
resource "tenzir_organization" "demo" {
  slug = "demo"
  name = "Demo"
  plan = "provisioned"
}

# Anyone else omits the plan, and becomes the first owner.
resource "tenzir_organization" "acme" {
  slug = "acme"
}
