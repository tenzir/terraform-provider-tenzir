terraform {
  required_providers {
    tenzir = {
      source = "tenzir/tenzir"
    }
  }
}

provider "tenzir" {
  # Both attributes can also be set via the TENZIR_PLATFORM_ENDPOINT and
  # TENZIR_PLATFORM_ID_TOKEN environment variables. The OIDC ID token is
  # exchanged for short-lived, workspace-scoped API keys as needed.
  endpoint = "https://api.platform.example.tenzir.com"
}
