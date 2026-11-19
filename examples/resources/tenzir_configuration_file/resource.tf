resource "tenzir_configuration_file" "ingest" {
  organization_id = data.tenzir_organization.acme.id
  workspace_id    = tenzir_workspace.soc.id
  branch          = "main"
  path            = "pipelines/ingest.tql"
  content         = <<-TQL
    ---
    name: Ingest Suricata alerts
    ---
    from_file "/var/log/suricata/eve.json", follow=true {
      read_suricata
    }
    publish "suricata"
  TQL
  commit_message = "Ingest Suricata alerts"
}

resource "tenzir_configuration_file" "settings" {
  organization_id = data.tenzir_organization.acme.id
  workspace_id    = tenzir_workspace.soc.id
  path            = "tenzir.yaml"
  content         = file("${path.module}/tenzir.yaml")
}
