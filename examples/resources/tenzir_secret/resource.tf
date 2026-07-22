# Recommended: source the value from an ephemeral, sensitive variable and
# write it through `value_wo`, which never lands in the plan or state.
# Terraform cannot detect changes to a write-only value on its own, so bump
# `value_wo_version` (an opaque marker, e.g. a counter or timestamp) whenever
# `value_wo` changes to trigger an in-place update. Requires Terraform >=
# 1.11, the first release with write-only attribute support.
variable "api_key" {
  type      = string
  ephemeral = true
  sensitive = true
}

resource "tenzir_secret" "example" {
  workspace_id     = tenzir_workspace.example.id
  name             = "api-key"
  value_wo         = var.api_key
  value_wo_version = "1" # bump this whenever var.api_key changes
}

# Alternative: `value` works on any Terraform version and can be read back
# from the state (e.g. in `terraform show`), but the plaintext is then
# stored in the state, so the state itself must be treated as sensitive.
# resource "tenzir_secret" "example" {
#   workspace_id = tenzir_workspace.example.id
#   name         = "api-key"
#   value        = var.api_key
# }
