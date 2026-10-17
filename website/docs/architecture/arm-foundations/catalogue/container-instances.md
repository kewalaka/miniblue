# Container Instances

> Engine fit: hooks for `Microsoft.ContainerInstance/containerGroups` CRUD (passthrough plus defaults), engine-gap for the `start` / `stop` / `restart` actions (`Type.Actions`), bespoke for the container logs route. miniblue today: hand-written service `internal/services/aci` (Docker-backed when a Docker socket exists, stub otherwise). **miniblue has zero ACI tests today**: no `tests/aci*` file and no `azurerm_container_group` Terraform fixture. Suggested wave: PLAN wave 4.

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.ContainerInstance/containerGroups` | 2021-10-01, 2022-09-01, 2023-05-01, 2025-09-01, 2026-07-01 (latest in spec) | RG; list by RG and by subscription | `containers/{c}/logs` | PATCH (tags only); POST `start`, `stop`, `restart` | passthrough + hooks, engine-gap for actions | `Hooks.Defaults` (`provisioningState`, `ipAddress` fields), `PatchMode` merge, `Type.Actions`; sub-list and parent checks are already engine behaviour (ADR-0003) | M | S |
| `containerGroups/{g}/containers/{c}/logs` | same | under a container group | none | GET (`tail`, `timestamps`) | bespoke (tiny) | none (chi route) | L | S |

Out of scope: `ngroups`, `sandboxGroups` and `containerGroupProfiles` also appear in the spec and have no source scenarios.

## miniblue gaps

- No tests of any kind (see above).
- `buildResponse` (`internal/services/aci/handler.go:265-355`) drops `tags`, `restartPolicy`, `resources` and `dnsNameLabel` from the stored input. azurerm refresh would show drift.
- Routes cover only list-by-RG, PUT, GET and DELETE (`handler.go:40-44`). Missing: list by subscription, PATCH, `start` / `stop` / `restart`, container logs, RG-existence check on PUT.
- `ipAddress.ip` is hard-coded to `127.0.0.1` (`handler.go:318`), and the default location is `eastus` (`handler.go:270`).
- The internal `_containerID` is written into the API response (`handler.go:350`).
- DELETE returns 404 for a missing group (`handler.go:237-251`); real allows 200/202/204 and treats a missing group as 204 `(verify)`.
- PUT returns 201 on create and 200 on replace (`handler.go:208-210`), which matches the spec and ADR-0006.
- Docker side effects (`dockerRun`, `handler.go:74`) have no hook in `Hooks{Validate, Defaults}` `(verify)`. Stub mode is the testable baseline.

## Scenarios

### containerGroups

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T69 | PUT a Linux group with `restartPolicy=Always` and one container (image, `resources.requests` cpu and memoryInGB) | PUT => 201 (spec 200/201); `osType`, `restartPolicy`, `containers[].properties.image` and `resources` echoed; `provisioningState=Succeeded` | T2 | Spec requires `properties.containers`; each container needs `name` and `properties`; `resources.requests` is required. Error code for a missing field `(verify)` |
| G1-T70 | PUT with a public IP, `dnsNameLabel` and port 80/TCP | `ipAddress.type=Public`, `dnsNameLabel` echoed, `ports` echoed, `ip` non-empty, `fqdn` derived from the label | T2 | Spec requires `ipAddress.ports` and `type`; `dnsNameLabel`, `fqdn`, `ip` are optional. `type` enum values and fqdn format `(verify)`. Covered end to end by the T3 row below |
| G1-T71 | PUT with tags, then GET | Tags are returned unchanged | T2 | Dropped by miniblue today (`handler.go:265-355`) |
| G1-T72 | GET a missing group; PUT into a missing RG | GET => 404 `ResourceNotFound`; PUT => 404 `ResourceGroupNotFound` | T1 | Generic engine checks |
| G1-T73 | List by RG and list by subscription | Both 200 and contain the group | T1 | Sub-wide list is engine behaviour (ADR-0003). `GET /subscriptions/{s}/providers/Microsoft.ContainerInstance/containerGroups` |
| G1-T74 | PUT the same group again with a change | PUT => 200 (replace); GET shows the change; PATCH with tags only => 200 | T2 | Real ACI rejects updates to some immutable properties `(verify)`; assert only on a mutable field such as tags. PATCH is tags-only in the spec |
| G1-T75 | POST `start`, `stop`, `restart`; same on a missing group | Each => 204 with no body (spec: `start` 202/204, `stop` 204, `restart` 204); missing group => 404 `ResourceNotFound` | T2 | `Type.Actions`; matches the ADR-0006 "204 without body" action form. Do not assert instance state transitions `(verify)` |
| G1-T76 | DELETE a group, then GET | DELETE => 200, 202 or 204; GET => 404 `ResourceNotFound`; DELETE of a missing group => 204 `(verify)` | T1 | Sync engine returns 200 or 204 |
| G1-T78 | `azurerm_container_group` x3: no public IP + `Always`, public IP + `dns_name_label` + `OnFailure`, tagged | apply then refresh => clean plan; `ip_address_type`, `dns_name_label`, `restart_policy`, `os_type` and tags echoed | T3 | New fixture under `tests/terraform/azurerm/`. azurerm refresh sensitivity to `ipAddress` when no IP is requested `(verify)` |

### containers/logs

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T77 | GET `containerGroups/{g}/containers/{c}/logs` | 200 with `{"content": "<text>"}`; `tail` limits to the last N lines; `timestamps=true` prefixes each line | T2 | Bespoke. Assert the JSON shape, not a placeholder string. Content in stub mode is implementation-defined |

## Pitfalls

- `tags`, `restartPolicy`, `resources` and `dnsNameLabel` must round-trip, otherwise azurerm shows a diff on every refresh.
- Do not hard-code `ipAddress.ip`. A public group returns a real-looking address and an `fqdn` built from `dnsNameLabel`.
- `start`, `stop` and `restart` return 204, not 200 with a body.
- Logs are `{"content": "..."}`, not a bare string.
- DELETE is idempotent: a missing group is not a 404.
- In the 2026-07-01 spec `location` is optional on the group (it is a proxy resource there); older versions treat it as required `(verify)`. Do not assert a 400 on a missing location.
- Never leak internal fields (for example the Docker container id) into the response.
- PUT is a full replace. PATCH changes tags only.
- Create is an LRO in the spec (200/201); the sync engine relies on SDK pollers accepting a terminal 201 `(verify)`.
- Docker-backed runs are nondeterministic in CI. Write acceptance tests against the stub-mode shape.

## Dropped

| ID | Reason |
|---|---|
| none | All ten source ids for this service are in Scenarios. |
