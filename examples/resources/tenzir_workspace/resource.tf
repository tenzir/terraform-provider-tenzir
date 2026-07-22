# Workspaces are owned by the organization of the authenticated user. When
# the organization is managed in the same configuration, declare the
# dependency explicitly.
resource "tenzir_workspace" "example" {
  name       = "Security Operations"
  icon_url   = "https://example.com/icon.png"
  depends_on = [tenzir_organization.example]
}
