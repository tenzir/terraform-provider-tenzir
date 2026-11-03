# Plan: provider v2 for the new Tenzir platform

The new platform lives in `tenzir/mono` under `platform/`. Its REST API is
declared with Effect `HttpApi` in `platform/services/shared/api/groups/*.ts`
and served under `/api`. A running backend serves the OpenAPI document at
`/api/openapi.json`.

## Versioning

- The provider name stays `tenzir/tenzir`. The next release is `v1.0.0`, a
  breaking release that removes every v1-platform resource.
- `v1.0.0` keeps the Go module path free of `/v2`. Nobody imports this
  module, but the path must match the major version.
- A `release/v0.1` branch receives fixes for the old platform. Users stay on
  the old platform with `version = "~> 0.1"`.
- The README and the registry docs gain an upgrade note: this is a new
  platform, there is no state migration, and old resources must be
  re-imported or recreated.

## Provider configuration

The attributes mirror the platform CLI (`platform/tools/platform-cli/README.md`):

| Attribute | Environment variable | Meaning |
|---|---|---|
| `url` | `TENZIR_PLATFORM_URL` | Platform base URL; default `https://platform.tenzir.dev` |
| `api_key` | `TENZIR_API_KEY` | `tnzu_…` key, sent as `x-tenzir-api-key` |
| `token` | `TENZIR_TOKEN` | OIDC bearer token, sent as `Authorization: Bearer` |

- The provider reads `GET /.well-known/tenzir-platform` to find `apiUrl`.
- `api_key` and `token` conflict, because the platform rejects requests that
  carry both.
- `stage`, `service_token`, `organization_id` and the key-exchange dance of
  v1 all go away.

### Identity

For now, Terraform logs in with a user's API key. Its policy is intersected
with that user's permissions, so the user needs every right Terraform uses.
Service accounts replace this later, under a separate plan; the provider
needs no change for that beyond accepting their keys.

Locking a workspace to Terraform should be a role grant to the Terraform
identity, not a "managed" flag.

## Client

- `internal/client/` is rewritten as a small hand-written Go client: one file
  per API group, typed request and response structs, and one error type that
  carries the HTTP status and the Effect error `_tag`.
- Generating Go from `/api/openapi.json` is not worth it yet. Effect emits
  OpenAPI 3.1 with tagged unions, tuple-encoded policies and three-state
  optionals, which Go generators handle poorly.
- A contract test fetches `openapi.json` from the dev stack and checks that
  every path and method the client uses still exists. This catches renames on
  the platform side.

## Resources

IDs are UUIDs in state. Paths accept an ID or a slug, so a slug change never
breaks state. Every resource supports `terraform import`.

### Phase 1: workspace and access

| Resource | API | Notes |
|---|---|---|
| `tenzir_workspace` | `…/workspaces`, `…/update`, `DELETE` | Delete fails while deployments exist; surface the API's error |
| `tenzir_role` | `…/roles`, `…/update`, `DELETE` | `policy` is a JSON string with semantic equality, because the server normalizes policies |
| `tenzir_role_assignment` | `PUT`/`DELETE …/people/:user/roles/:role` | Create and delete only; both are idempotent |
| `tenzir_api_key` | `POST /users/:user/api-keys`, `revoke`, `set-policy`, `rename` | `key` is only known at create time; delete revokes |
| `tenzir_secret` | org, workspace and deployment scopes | One resource with `organization`, `workspace` and `deployment` attributes; the most specific one sets the scope. `value` is write-only; drift is detected through `version` |
| `tenzir_ingest_endpoint` | `…/ingest-endpoints` | `name` forces replacement; `token` is sensitive and readable |

Data sources: `tenzir_organization`, `tenzir_workspace`, `tenzir_people`,
`tenzir_roles`, `tenzir_deployment_types`, `tenzir_deployment_images`.

`tenzir_organization` is only a data source in phase 1. Creating an
organization needs a user identity, and its delete is refused while the
organization holds anything.

### Phase 2: deployments

| Resource | API | Notes |
|---|---|---|
| `tenzir_deployment` | `POST …/deployments`, `tracker`, `DELETE` | Self-hosted. `name` forces replacement, because there is no rename. `branch` and `auto_deploy` update through `PUT tracker`. An optional `tenzir_deployment_key` resource rotates and exposes the key |
| `tenzir_hosted_deployment` | `POST …/hosted-deployments`, `type`, `tracker`, `DELETE` | `size` updates in place (restart); `image` and `region` force replacement. Create waits until `reachable`, with a timeout |

The two kinds stay separate resources: their create calls and mutable fields
differ.

### Phase 3: configuration

Pipelines and packages live in the workspace's git repository. In phases 1
and 2, Terraform only points deployments at a branch.

Phase 3 adds `tenzir_configuration_file`: one file on one branch of a
workspace, keyed by `(workspace, branch, path)`. Create, update and delete
each write through the draft API (`save` with the current `headSha`, then
`commit`), and read takes the file at the branch tip. Drafts belong to the
calling user, so the provider discards its draft on error. A `headSha`
conflict retries once against the new tip. Package files stay out of scope
until deployments accept them.

## Platform changes this needs (in `tenzir/mono`)

1. `GET /api/deployments/:id` returns the tracker, hosting kind, size and
   region, so `Read` can detect drift without a list call.
2. A single-secret `GET` for each scope, instead of filtering the list.
3. A deployment rename, so `name` needn't force replacement.
4. Later: a member invitation API, for a `tenzir_organization_member` resource.

## Tests

- Unit tests cover the client's error decoding and policy equality.
- Acceptance tests (`TF_ACC=1`) run against a local dev stack from
  `tenzir/mono`. One test per resource does create, import, update and
  delete. They don't restate the schema.
- CI runs the acceptance tests against a preview environment once service
  accounts exist.

## Access paths

- REST API: yes; the provider is a REST client.
- RBAC: yes; every call goes through the identity's policy, and the docs list
  the actions each resource needs.
- MCP, web UI, CLI and TQL: not affected.
- Docs: regenerate `docs/` with `tfplugindocs`, and rewrite `examples/` for
  the new resources.

## Order of work

1. Delete the v1 resources and the v1 client; add the new provider
   configuration and the discovery call.
2. Client and phase 1 resources, with acceptance tests against the dev stack.
3. Platform: endpoint changes 1–3, in `tenzir/mono`.
4. Phase 2 resources.
5. Phase 3 resource.
6. Docs, examples, upgrade note, `release/v0.1` branch, tag `v1.0.0`.
