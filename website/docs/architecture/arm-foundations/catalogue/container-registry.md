# Container Registry

> Engine fit: hooks for `Microsoft.ContainerRegistry/registries` (defaults, name validation, identity) with engine-gaps for credential actions (`Type.Actions`) and `checkNameAvailability`; hooks for `replications`; bespoke for runs and the OCI data plane. miniblue today: hand-written service `internal/services/acr`. Suggested wave: PLAN wave 4 (ARM only; the data plane never migrates).

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.ContainerRegistry/registries` | 2022-12-01, 2023-07-01, 2025-04-01, 2025-11-01 (latest stable in spec) | RG; list by RG and by subscription | `replications`, `tasks`, `runs`, `tokens` `(verify)` | PATCH; POST `listCredentials`, `regenerateCredential`, `generateCredentials`, `scheduleRun`; GET `listUsages` | hooks + engine-gap | `Hooks.Validate` (name), `Hooks.Defaults` (`loginServer`, `creationDate`, access defaults, sku tier), `DefaultIdentity`, `PatchMode` merge, `Type.Actions` (lands with the first credential consumer) | H | M |
| `Microsoft.ContainerRegistry/checkNameAvailability` | same | subscription (POST) | none | POST | engine-gap | `CheckName` if it covers a subscription-scoped POST whose body carries `type`, otherwise a provider-level chi route `(verify)` | M | S |
| `registries/replications` | same | under a registry | none | none | hooks | parent check (engine) | L | S |
| `registries/tasks` | spec lives in the registry-tasks directory, not checked `(verify)` | under a registry | none | `(verify)` | hooks + engine-gap | `Type.Actions` `(verify)` | L | M |
| `registries/runs`, `scheduleRun`, `listLogSasUrl` | same `(verify)` | under a registry | none | POST `scheduleRun`, `listLogSasUrl`; PATCH | bespoke | none | L | L |
| OCI data plane (`/v2/...`) | n/a (OCI distribution spec) | registry host | none | push, pull, catalog, tags | bespoke | none | M | L |

## miniblue gaps

- `checkNameAvailability` never reports `AlreadyExists`: `internal/services/acr/handler.go:224` type-asserts `item.(Registry)`, but stored values are maps. Existing bug.
- `buildRegistryResponse` (`handler.go:63`) drops `tags` and `identity`, echoes `sku.tier = sku.name` (`handler.go:96`), hard-codes `adminUserEnabled=false` (`handler.go:101`) and a constant `creationDate` of `2026-01-01T00:00:00Z` (`handler.go:102`), and defaults `location` to `eastus` (`handler.go:68`).
- PUT (`handler.go:126`) ignores a body decode error and does no name or location validation. Responses are 201 on create and 200 on replace (`handler.go:140-142`); DELETE returns 202 (`handler.go:168`) and 404 for a missing registry.
- Missing: PATCH, `listCredentials`, `generateCredentials`, `regenerateCredential`, `listUsages`, tasks, runs, `tokens`, list by subscription, RG-existence check.
- `replications` is a stub that returns an empty list (`handler.go:48`, `ListReplications` at `handler.go:196`).
- Data-plane stubs live under `/acr/{registry}/v2/...` (`handler.go:53-56`: manifests and `tags/list` only). No blob upload, no `_catalog`, no token exchange.
- Existing coverage: `tests/acr_test.go` (`TestACRRegistry`, `TestACRRegistryARMPolicyAndRoleAssignmentMode`). No azurerm or azapi ACR fixture.

## Scenarios

### registries

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T79 | PUT a Standard registry | PUT => 201 (spec 200/201); `loginServer` ends `.azurecr.io` and contains the name; `adminUserEnabled=false`; `sku.name=Standard`, `sku.tier=Standard`; `provisioningState=Succeeded`; `publicNetworkAccess=Enabled`; `zoneRedundancy=Disabled`; `networkRuleBypassOptions=AzureServices`; `creationDate` is a current RFC 3339 timestamp | T2 | `Hooks.Defaults`. `sku` is required, `tier` is read-only. A domain-name-label scope may add a hash suffix to `loginServer`; assert "contains name" `(verify)` |
| G1-T80 | PUT with a name shorter than 5, longer than 50, or non-alphanumeric | 400 with an error code `(verify)` | T2 | `Hooks.Validate`. Name rule from the spec: 5-50 characters, `^[a-zA-Z0-9]*$`. Real code is likely `InvalidParameter` `(verify)`; do not assert a bespoke code |
| G1-T81 | PUT without `location`; PUT into a missing RG | missing location => 400 `LocationRequired` `(verify)`; missing RG => 404 `ResourceGroupNotFound` | T1/T2 | RG check is generic (T1). Location is required on `TrackedResource`; never default it |
| G1-T82 | PATCH `adminUserEnabled` true, then false | PATCH => 200 and GET reflects the flag. With the flag on, `listCredentials` returns a username equal to the registry name and two passwords; with it off the passwords are not available | T2 | `PatchMode` merge. Passwords never appear on GET. Spec lists PATCH as 200/201 (LRO) |
| G1-T83 | PUT a Premium registry with a SystemAssigned identity | `sku.name` and `sku.tier` are `Premium`; `identity.type=SystemAssigned` with `principalId` and `tenantId` present | T2 | `DefaultIdentity` seam. Today `identity` is dropped (`handler.go:63`) |
| G1-T84 | GET a missing registry | 404 `ResourceNotFound` | T1 | Generic |
| G1-T85 | List by RG, list by subscription, DELETE | Lists contain the registry; DELETE => 200, 202 or 204; GET after => 404 `ResourceNotFound` | T1 | Sync engine returns 200 or 204 |
| G1-T86 | POST `listCredentials` with admin enabled, with admin disabled, then after enabling | Enabled: 200 with `username` and `passwords` (`password`, `password2`, each with a `value`). Disabled: error with a code `(verify)`. Works again after PATCH enables it | T2 | `Type.Actions`. Azurerm reads these when `admin_enabled` is set `(verify)`, which makes this the first consumer that lets the seam land |
| G1-T87 | POST `generateCredentials` (all passwords, or one by `name`); missing registry | 200 (spec 200/202) with `username` and `passwords[]` (one entry when a name is given); missing registry => 404 `ResourceNotFound` | T2 | Real input is `tokenId`, `expiry` and `name` (`password1` or `password2`), and the credentials belong to a registry token, not the admin user. Needs a `registries/tokens` type before it can be ported `(verify)` |
| G1-T88 | POST `regenerateCredential` with `name` = `password` or `password2` | 200 with a body in the `listCredentials` shape `(verify)`; only the named password changes; the other is stable; admin disabled => error with a code `(verify)` | T2 | `Type.Actions`. The old value must stop matching |
| G1-T89 | GET `listUsages` by sku | 200 `value[]` with `name`, `limit`, `currentValue`, `unit`. Documented limits: Basic storage 10 GiB / webhooks 2; Standard 100 GiB / 10; Premium 500 GiB / 500 | T2 | Spec shape verified. Limit values, unit string and which usage names appear `(verify)` |
| G1-T97 | `azurerm_container_registry` (Standard, `admin_enabled=true`) and an azapi basic registry | apply then refresh => clean plan; `login_server` contains the name; `admin_enabled` echoed | T3 | New fixtures `tests/terraform/azurerm/container_registry` and `tests/terraform/azapi/container_registry_basic`. Provider read calls beyond GET `(verify)` |

### checkNameAvailability

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T90 | POST `/subscriptions/{s}/providers/Microsoft.ContainerRegistry/checkNameAvailability` with a free, a used and an invalid name | Free => 200 `{nameAvailable: true}`. Used => `{nameAvailable: false, reason: AlreadyExists, message}`. Invalid (under 5, over 50, non-alphanumeric) => `{nameAvailable: false, reason: Invalid, message}` | T2 | Request `type` must be `Microsoft.ContainerRegistry/registries`. Names are global (DNS), so real Azure also reports names used in other subscriptions; assert only the in-store case |

### replications

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T91 | GET the replications list of a registry | 200 `value[]`. A Premium registry lists its home-region replication `(verify)` | T2 | Spec: replication PUT is 200/201 (LRO), PATCH exists, DELETE is 200/202/204. Basic and Standard behaviour `(verify)` |

### tasks and runs

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T92 | Tasks: PUT, GET, PATCH (`status`), DELETE, list; PUT without a task type | CRUD as for any child; missing task type => 400 `BadRequest` `(verify)` | T2 | Spec for tasks was not checked `(verify)`. May need `Type.Actions` for `listDetails` `(verify)` |
| G1-T93 | Runs: POST `scheduleRun`, GET and list runs, PATCH `isArchiveEnabled`, POST `listLogSasUrl` | `scheduleRun` yields a run whose `status` reaches `Succeeded` in the sync model; GET and list return it; PATCH persists `isArchiveEnabled`; `listLogSasUrl` => `logLink` | T2 | Bespoke. Real runs move through Queued, Running, Succeeded; the sync form returns a terminal state `(verify)`. The source also lists a plain `run` operation `(verify)` |

### Data plane (bespoke)

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T95 | OCI distribution API: `/v2/` auth challenge and token exchange; blob upload (initiate, patch, complete); manifest PUT, GET, HEAD, DELETE; `_catalog`; `tags/list` with `n` and `last` pagination; blob DELETE | Challenge => 401 with `WWW-Authenticate`; manifest and blob DELETE => 202; unknown manifest => 404; `tags/list` pages by `n` and `last` | T2 | Bespoke, never migrates (ADR-0007). Real Azure serves this on `{name}.azurecr.io`; miniblue serves a path form `/acr/{registry}/v2/...`. Exact status codes per the OCI spec `(verify)` |

## Pitfalls

- `location` is required on the registry. Real ARM returns 400 `LocationRequired` `(verify)`; do not default it to the RG location.
- Names are 5-50 alphanumerics and globally unique. The real error code for an invalid name on PUT is unconfirmed `(verify)`; `RegistryNameInvalid` is not a known real code.
- A missing registry is 404 `ResourceNotFound`, not a service-specific code such as `ContainerRegistryNotFound`. A missing replication is likewise `ResourceNotFound`.
- `sku.tier` is read-only and derived from `sku.name` (`Classic`, `Basic`, `Standard`, `Premium`).
- Passwords come only from `listCredentials`, `regenerateCredential` and `generateCredentials`, never from GET. `regenerateCredential` changes only the named password.
- `generateCredentials` takes a token id, so it is not the admin-user flow.
- PUT is a full replace and must keep `tags`, `identity` and `sku`. `creationDate` is the real creation time.
- PATCH and PUT are LROs in the spec (200/201). The sync engine relies on SDK pollers accepting a terminal response `(verify)`.
- `checkNameAvailability` is a POST at subscription scope, with `{nameAvailable, reason, message}` in the response. `AlreadyExists` must come from stored registries, not from a type assertion that never matches.
- The error code for `listCredentials` with admin disabled is unconfirmed `(verify)`. Bespoke upper-case codes are not ARM style.
- Replications are a Premium feature `(verify)`.

## Dropped

| ID | Reason |
|---|---|
| G1-T94 | Docker-backed run builds and multi-step task YAML need a Docker executor. Not portable. |
| G1-T96 | `docker push` and `docker pull` through the CLI need a Docker daemon and a host-routed login server. Not portable. |
