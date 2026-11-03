resource "tenzir_ingest_endpoint" "webhooks" {
  organization_id = data.tenzir_organization.acme.id
  name            = "webhooks"
  description     = "Alerts from the ticketing system"
  readable_by = {
    workspaces = [tenzir_workspace.soc.id]
  }
}

output "webhook_url" {
  value = tenzir_ingest_endpoint.webhooks.url
}
