# Terraform Provider for the Tenzir Platform

Manage workspaces, access, secrets, ingest endpoints, deployments and their
configuration files of the
[Tenzir Platform](https://docs.tenzir.com) with Terraform. The provider is
built on the
[Terraform Plugin Framework](https://developer.hashicorp.com/terraform/plugin/framework).

## Upgrading from 0.x

Version 1.0 targets the new Tenzir Platform and shares no resources with
0.x. There is no state migration: remove the old resources from your state,
then import or recreate them with the new ones. To stay on the old platform,
pin `version = "~> 0.1"`.

## Usage

```hcl
terraform {
  required_providers {
    tenzir = {
      source  = "tenzir/tenzir"
      version = "~> 1.0"
    }
  }
}

provider "tenzir" {}

data "tenzir_organization" "acme" {
  slug = "acme"
}

resource "tenzir_workspace" "soc" {
  organization_id = data.tenzir_organization.acme.id
  slug            = "soc"
}
```

The provider reads the platform address from `url` or `TENZIR_PLATFORM_URL`,
defaulting to `https://platform.tenzir.dev`, and finds the API through
`/.well-known/tenzir-platform` there. It authenticates with one of:

- `api_key` or `TENZIR_API_KEY`: a user API key (`tnzu_…`). Mint one with
  `tenzir api-key create` from the platform CLI.
- `token` or `TENZIR_TOKEN`: an OIDC bearer token the platform accepts.

An API key acts as its user, limited further by its own policy. The user
needs every permission that the managed resources need; each resource's
documentation lists them.

A platform administrator's key may set organizations up for others: create
one under a fixed ID. The key's policy must name that permission too,
`Root.provisionOrganization`.

### Resources

- `tenzir_workspace`
- `tenzir_organization`
- `tenzir_role` and `tenzir_role_assignment`
- `tenzir_api_key`
- `tenzir_secret`, at the organization, workspace or deployment level
- `tenzir_ingest_endpoint`
- `tenzir_deployment`, `tenzir_hosted_deployment` and `tenzir_deployment_key`
- `tenzir_configuration_file`, one file of a workspace's configuration branch

### Data sources

- `tenzir_organization` and `tenzir_workspace`
- `tenzir_people` and `tenzir_roles`
- `tenzir_deployment`, `tenzir_deployment_types` and `tenzir_deployment_images`

See [`docs/`](docs/) for every attribute, and [`examples/`](examples/).

## Development

- `main.go`: the provider server.
- `internal/provider/`: the provider, resources and data sources.
- `internal/client/`: a hand-written client for the platform API, one file
  per API group.

```sh
make build test
```

### Local testing with `dev_overrides`

To run a local build, install it into `$GOPATH/bin`:

```sh
make install
```

Then point Terraform at it in `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "tenzir/tenzir" = "/home/<you>/go/bin"
  }
  direct {}
}
```

With the override, `terraform plan` and `apply` use the local binary without
`terraform init`.

### Acceptance tests

Acceptance tests create, import, update and delete real resources. Run them
against a local platform from `tenzir/mono`:

```sh
cd platform
nix develop .#platform -c tools/bin/dev up -d --no-tui --session tf-provider
```

Sign in at `/auth/login` and mint an API key, for example with the platform
CLI. Then:

```sh
export TENZIR_PLATFORM_URL=http://localhost:3100
export TENZIR_API_KEY=tnzu_...
export TENZIR_TEST_ORGANIZATION=local-dev
make testacc
```

With `TF_ACC` set, `TestContract` also checks every API operation the client
calls against the platform's `openapi.json`.

### Documentation

`docs/` is generated from schema descriptions and `examples/`:

```sh
make docs
```
