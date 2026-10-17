# Redis

> Engine fit: hooks for `redis` plus `Type.Actions` for `listKeys` and `regenerateKey`; passthrough child for `firewallRules`. miniblue today: hand-written service `internal/services/redis` (PUT, GET, DELETE, list by RG, static `listKeys`); PUT ignores the request body, see below. Suggested wave: wave 2 (PLAN phase 4); `firewallRules` as a new type after G2.

Scope: the ARM control plane only. The RESP data plane is not ARM; miniblue proxies connectivity to a real Redis through `REDIS_URL` and does not implement the protocol.

Spec facts used below are confirmed in the `Microsoft.Cache` stable spec (latest 2024-11-01, then 2024-03-01): create is a PUT (200 or 201, long-running); PATCH is 200 or 202; DELETE is 200, 202 or 204; list exists by RG and by subscription; the create body requires `location` and `properties.sku` with `name`, `family` and `capacity`; sku `name` is `Basic`, `Standard` or `Premium`; `family` is `C` or `P`; capacity is 0 to 6 for `C` and 1 to 4 for `P`; `enableNonSslPort` defaults to false; `minimumTlsVersion` is `1.0`, `1.1` or `1.2`; `publicNetworkAccess` is `Enabled` or `Disabled`; `accessKeys` appear only on create and update responses; `POST listKeys` returns `primaryKey` and `secondaryKey`; `POST regenerateKey` takes `{keyType: Primary|Secondary}`; `firewallRules` require `properties.startIP` and `endIP`.

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.Cache/redis` | 2024-11-01, 2024-03-01 (spec dir; which azurerm pins is `(verify)`) | RG; list by subscription | `firewallRules`; also `accessPolicies`, `accessPolicyAssignments`, `linkedServers`, `patchSchedules`, `privateEndpointConnections` (not scoped) | `POST listKeys`, `POST regenerateKey`; also `flush`, `forceReboot`, `import`, `export` (not scoped) | hooks | `Type.Actions` (lands with this first `listKeys` consumer), `Defaults` (hostName, ports, sku echo), `PatchMode` merge, `ListSubscription` (PLAN 1.2), RG existence, `Provisioned` | H | S to M |
| `redis/firewallRules` | same | child of redis | none | none | passthrough child | parent existence (`ParentResourceNotFound`), `Validate` (`startIP`, `endIP`) | M | S |
| Provider-level `POST /subscriptions/{s}/providers/Microsoft.Cache/checkNameAvailability` | same | subscription | none | n/a | chi route | none | L | S |

Path casing for the provider-level name check is `(verify)`.

## miniblue gaps

Ordered by value.

- **PUT ignores the request body.** `CreateOrUpdateCache` (`internal/services/redis/handler.go:99-126`) never reads `r.Body`. `buildCacheResponse` (`handler.go:69-97`) hard-codes location `eastus`, sku `Basic` / `C1`, `enableNonSslPort` true, `redisVersion` 7.2 and `publicNetworkAccess` `Enabled`, and drops tags. Everything the client sends is lost, so azurerm shows permanent drift.
- **`hostName` is not the cache's.** `redisHostPort` (`handler.go:40`) yields the `REDIS_URL` host or `localhost`, not `{name}.redis.cache.windows.net`. Derive it from the cache name; keep the `REDIS_URL` target as the proxy destination.
- **`listKeys` returns static keys** (`miniblue-redis-key` and `-2`, `handler.go:160-174`); there is no `regenerateKey`.
- **No `firewallRules`, no PATCH, no list by subscription, no RG-exists check.**
- **DELETE** answers 200 and 404 on a missing cache (`handler.go:141-151`); real Azure answers 200 when it existed and 204 when it did not.
- **Connectivity check.** When `REDIS_URL` is set and unreachable, PUT returns 503 `RedisUnreachable` (`handler.go:104-113`). Keep this as an opt-in `Validate` hook, not a default.
- **No tests.** The route table is at `handler.go:24-34`. No file under `tests` exercises `Microsoft.Cache` redis (the only match is an unrelated VM identity test).

## Scenarios

### redis

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T121 | Create a cache; the request body is honoured. | `PUT redis/{n}` => 201; `properties.hostName` contains the cache name; `port` 6379; `sslPort` 6380; `provisioningState` `Succeeded`; sku, tags and location echo the request. | T2 | Fails today: body ignored. Fix first; T127 depends on it. |
| G2-N15 | `new`: required body fields. | `PUT` without `location`, or without `properties.sku`, or with a sku name outside `Basic`, `Standard`, `Premium` => 400. | T2 | `new`. Code (`InvalidRequestContent` per ADR-0006 versus a provider code) is `(verify)`. |
| G2-T122 | Get, update tags, delete, list. | `GET` => 200; `PATCH` `{tags}` => 200 with tags updated; `DELETE` => 200 then `GET` => 404 `ResourceNotFound`; list by RG and by subscription include every cache. | T1 | Real PATCH and DELETE are long-running; sync-only returns 200. Subscription list needs PLAN 1.2. |
| G2-T123 | Access keys. | `POST listKeys` => 200 with `primaryKey` and `secondaryKey`, identical across calls; `POST regenerateKey` `{keyType: Primary}` => 200 and only the primary changes; `Secondary` changes only the secondary. | T2 | Needs `Type.Actions`. Persist keys on the resource, do not return constants. An unknown `keyType` => 400 `(verify)`. |
| G2-T127 | azurerm create and destroy `azurerm_redis_cache`. | Apply and destroy succeed; `hostname` contains the name; `ssl_port` 6380; `minimum_tls_version` round-trips; `non_ssl_port_enabled` round-trips (default false). | T3 | New fixture. Needs T121. TLS is reflected, not enforced. |
| G2-T128 | az CLI create, show, delete, list, `list-keys`, `regenerate-keys`. | Equivalent REST calls succeed with the same shapes as T121 to T123. | T4 | The CLI polls long-running operations; sync-only answers immediately. |

### firewallRules

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T124 | Rule lifecycle. | `PUT redis/{c}/firewallRules/{r}` => 201; update the range => 200; `GET` round-trips `properties.startIP` and `endIP`; `DELETE` => 200 then `GET` => 404; list shows current rules. | T2 | Passthrough child. |
| G2-T125 | Invalid rule body. | `PUT` without `properties`, or with a blank `startIP` or `endIP` => 400 `InvalidRequestContent` `(verify)`. | T2 | Spec marks both IPs required. |
| G2-T126 | Rule under a missing cache. | `PUT` or `GET` => 404 `ParentResourceNotFound` (engine rule); the real provider may return `ResourceNotFound` `(verify)`. | T1 | Engine parent-exists check. |

## Pitfalls

- `enableNonSslPort` defaults to false in the spec; a hard-coded true makes azurerm report drift on every plan.
- The create body requires `location` and the full `sku` triple; a missing one is a 400, not a silently applied default.
- Real DELETE returns 200 when the cache existed and 204 when it did not; a 404 on a missing cache is wrong.
- PUT is 201 on create and 200 on replace.
- Access keys appear in the create response and in `listKeys` only; a plain `GET` does not return them.
- Keys must be stable across `listKeys` calls and change only on `regenerateKey`, and only the targeted key.
- A child (`firewallRules`) with a missing parent is 404 `ParentResourceNotFound` in the engine; the real code may differ `(verify)`.
- A missing or malformed api-version is 400; any well-formed version is accepted.
- `hostName` and `sslPort` are what clients dial. If they are not reachable, azurerm succeeds but applications fail.

## Dropped

| ID | Reason |
|---|---|
| G2-T129 | RESP2 command suite (`SET`, `GET`, `HSET`, `INCR`, and so on). Data plane protocol; miniblue defers to a real Redis through `REDIS_URL`. |
