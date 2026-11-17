# A platform administrator sets up an organization on the provisioned plan,
# under a fixed ID; without `id`, the provider generates one. It starts
# without members: see tenzir_organization_member for making the managing
# account its owner.
resource "tenzir_organization" "demo" {
  id   = "01a1a205-9e4b-76ab-a7ba-cb8a5c23e632"
  slug = "demo"
  name = "Demo"
  plan = "provisioned"
}

# Anyone else omits the ID and the plan, and becomes the first owner.
resource "tenzir_organization" "acme" {
  slug = "acme"
}
