# Terraform Provider for the Tenzir Platform

A Terraform provider to manage Tenzir Platform resources, built on the
[Terraform Plugin Framework](https://developer.hashicorp.com/terraform/plugin/framework).

## Resources

- `tenzir_organization` — an organization; the authenticated user becomes
  its initial admin.
- `tenzir_workspace` — an org-owned workspace.
- `tenzir_node` — a node registration inside a workspace.

## Authentication

Platform user keys are valid for exactly one workspace, so whichever
credential is configured, the client exchanges it for an account-level key
plus one key per workspace it touches, caching keys until shortly before
expiry.

### Credential model

The provider supports three credential modes; set exactly one:

- **User-identity credentials** (`id_token` /
  `TENZIR_PLATFORM_ID_TOKEN`): an OIDC ID token from the platform's login
  IdP (e.g. from the `tenzir-platform` CLI token cache). Full access,
  including creating and deleting organizations. This is the only credential
  that can manage the `tenzir_organization` *resource*, since org-scoped
  credentials cannot create the organization they are scoped to. Exchanged
  via `/authenticate` and `/switch-tenant`.
- **Service tokens** (`service_token` /
  `TENZIR_PLATFORM_SERVICE_TOKEN`): a long-lived `tzst_...` token minted by
  an org admin (org settings → Service Tokens). Manages workspaces, nodes,
  and org settings inside that organization; reference the organization via
  the `tenzir_organization` *data source*. Exchanged via
  `/service-token/authenticate`.
- **Federated workload identity** (`id_token` together with
  `organization_id` / `TENZIR_PLATFORM_ORGANIZATION_ID`): an OIDC token from
  an external issuer (e.g. GitHub Actions) that matches one of the
  organization's federated credential configurations (org settings →
  Federated Credentials). Same scope as service tokens, but with no stored
  secret. Exchanged via `/federated/authenticate`.

Following the pattern of providers like GitHub's (org-scoped tokens for
day-to-day resources, enterprise/site-admin credentials for container-level
resources), a possible further tier — a deployment-admin credential for
self-hosted installations that provisions organizations declaratively — is
intentionally left open in this design but not implemented.

## Layout

- `main.go` — provider server entrypoint.
- `internal/provider/` — provider, resources, and data sources.
- `internal/client/` — HTTP client for the platform API.
- `examples/` — example configurations, also used by `tfplugindocs` to
  generate registry documentation.

## Development

Build and run unit tests:

```sh
make build test
```

### Local testing with `dev_overrides`

Terraform normally downloads providers from a registry. To test a local
build instead, install it into `$GOPATH/bin` and point Terraform at it:

```sh
make install
```

Then add to `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "tenzir/tenzir" = "/home/<you>/go/bin"
  }
  direct {}
}
```

With the override active, `terraform plan`/`apply` use the local binary
directly — no `terraform init` needed (it will even warn if you run it).

### Acceptance tests

Acceptance tests perform real API calls against a live platform:

```sh
export TENZIR_PLATFORM_ENDPOINT=...
export TENZIR_PLATFORM_API_KEY=...
make testacc
```

### Documentation

Registry docs are generated from schema descriptions and `examples/`:

```sh
make docs
```
