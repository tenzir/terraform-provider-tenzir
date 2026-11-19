data "tenzir_organization" "acme" {
  slug = "acme"
}

resource "tenzir_workspace" "soc" {
  organization_id = data.tenzir_organization.acme.id
  slug            = "soc"
  name            = "Security Operations"
}
