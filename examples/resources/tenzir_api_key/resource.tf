# A key that may only list its owner's API keys.
resource "tenzir_api_key" "ci" {
  name = "ci"
  policy = jsonencode([
    [{ target = { _tag = "User" }, relationships = ["self"] }, ["User.listApiKeys"]],
  ])
}

output "ci_api_key" {
  value     = tenzir_api_key.ci.key
  sensitive = true
}
