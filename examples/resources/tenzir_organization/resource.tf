# A platform administrator sets up an organization under a fixed ID, without
# becoming a member of it.
resource "tenzir_organization" "demo" {
  id   = "01a1a205-9e4b-76ab-a7ba-cb8a5c23e632"
  slug = "demo"
  name = "Demo"
}

# Anyone else omits the ID, and becomes the organization's first owner.
resource "tenzir_organization" "acme" {
  slug = "acme"
}
