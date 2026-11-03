data "tenzir_workspace" "default" {
  organization_id = data.tenzir_organization.acme.id
  slug            = "default"
}
