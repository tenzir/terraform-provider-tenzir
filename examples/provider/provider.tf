terraform {
  required_providers {
    tenzir = {
      source  = "tenzir/tenzir"
      version = "~> 1.0"
    }
  }
}

# Reads TENZIR_PLATFORM_URL and TENZIR_API_KEY (or TENZIR_TOKEN) from the
# environment when the attributes are left out.
provider "tenzir" {
  url = "https://platform.tenzir.dev"
}
