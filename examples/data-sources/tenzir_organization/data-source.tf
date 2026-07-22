# Reference an organization that is managed outside of Terraform. This is
# the typical pattern when the provider runs with org-scoped credentials,
# which cannot create organizations.
data "tenzir_organization" "main" {
  id = "org-abc123"
}

resource "tenzir_workspace" "example" {
  name = "Security Operations"
  # The workspace is created in the organization of the authenticated
  # credential, which for org-scoped credentials is data.tenzir_organization.main.
}
