# SQL

> Engine fit: hooks (servers, databases), passthrough child (firewallRules), engine-gap (singleton children). miniblue today: hand-written service `internal/services/sqldb` (stub, no tests). Suggested wave: PLAN wave 4 for servers and databases; `firewallRules` as a new type after G2.

Paths below are relative to `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Sql`. Spec facts come from `Microsoft.Sql/SQL` in the Azure REST API specs (latest stable `2025-01-01`, previews up to `2026-08-01-preview`).

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `servers` | real latest stable `2025-01-01`; azurerm pin `(verify)` | RG; list by RG and by subscription | databases, firewallRules, singleton children | PATCH (merge); subscription-level `checkNameAvailability` POST | hooks | `Hooks.Defaults` (redact password, keep immutable login, FQDN), `PatchMerge`, subscription-wide list (`Under`), global-unique name has no seam (hook or bespoke) | H | M |
| `servers/databases` | same | server | singleton children | PATCH (merge) | hooks | `Hooks.Defaults` (collation, status, size defaults), `PatchMerge`, parent-exists (default) | H | M |
| `servers/firewallRules` | same (PUT 200/201, DELETE 200/204, both sync in the spec) | server | none | list; bulk `PUT` on the collection replaces all rules `(verify)` | passthrough | child type, parent-exists (default) | H | S |
| Singleton children: `connectionPolicies/default`, `sqlVulnerabilityAssessments/default` (server); `transparentDataEncryption/current`, `securityAlertPolicies/default`, `backupShortTermRetentionPolicies/default`, `backupLongTermRetentionPolicies/default` (database) | same | server, database | none | GET returns defaults without a prior PUT | engine-gap: singleton | no seam: bespoke route, or `Defaults` that seeds on first GET `(verify)` | H (azurerm reads them) | M |
| `virtualNetworkRules`, `elasticPools`, `administrators`, `auditingSettings`, `failoverGroups`, `outboundFirewallRules` | same | server | none | none | passthrough | child types, only when a TF case needs them | L-M | S each |

## miniblue gaps

Ordered by value.

- Server PUT stores only `location` and `administratorLogin`; tags and every other property are dropped (`handler.go:57-71`). Database PUT drops tags and returns a fixed body (Basic sku, status Online).
- FQDN is hard-coded `localhost` (`handler.go:68`).
- No PATCH, no list by subscription, no `firewallRules`, no singleton children. azurerm reads the singletons and `azurerm_mssql_firewall_rule` has no endpoint to call.
- DELETE of an existing server or database returns 202 with no `Location` (`handler.go:150`, `handler.go:211`); DELETE of a missing one returns 404 (`handler.go:144-146`). Real Azure returns 204 for a missing resource.
- Server PUT does not check that the resource group exists; a missing `location` silently defaults to `eastus` (`handler.go:50-52`).
- Both types are in the RG cascade list (`resourcegroups/handler.go:165-166`); `firewallRules` and singletons would need their own prefix, or `DeleteTree` once on the engine.
- No tests for the package.

## Scenarios

### Servers

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T56 | Create then update a server returns a full server body | `PUT servers/{s}` => 201 then 200; body has `fullyQualifiedDomainName`, `version` `12.0`, `state` `Ready`, `publicNetworkAccess` `Enabled`, `provisioningState` `Succeeded`, `type` `Microsoft.Sql/servers` | T2 | Spec lists 200/201/202; sync mapping per ADR 0006. `state` and `version` defaults `(verify)` |
| G3-T57 | Tags are stored and returned; a later PUT with new tags updates them | `PUT` then `GET` => `tags` equal to the request | T2 | miniblue drops tags today |
| G3-T58 | `administratorLoginPassword` is never returned on PUT, GET or list; `administratorLogin` is | `PUT`/`GET`/`GET list` => no `administratorLoginPassword` key anywhere in the body; `administratorLogin` present | T2 | Spec marks the password create/update-only (`x-ms-secret`). Engine-gap: redact; strip it in a `Defaults` hook so it is never persisted |
| G3-T59 | PUT in a missing resource group fails | `PUT servers/{s}` in unknown RG => 404 `ResourceGroupNotFound` | T1/T2 | Covered by the engine default once migrated |
| G3-T60 | Server names are globally unique; a duplicate in another RG or subscription fails; deleting frees the name | second `PUT` => 409 (code `(verify)`); after `DELETE`, `PUT` => 201 | T2 | Engine-gap: global-unique. Status code on the second PUT is confirmed only as 409 by the source `(verify)` |
| G3-T61 | Invalid server names are rejected | name with uppercase, leading/trailing hyphen, or >63 chars => 400 (code `(verify)`) | T2 | Rule (1-63 chars, lowercase letters, digits, hyphen, no leading or trailing hyphen) is not confirmed in the spec `(verify)` |
| G3-T62 | PATCH merges tags and properties | `PATCH servers/{s}` => 200 with merged body; missing server => 404 `ResourceNotFound` | T1/T2 | `PatchMerge`; the generic row is a conformance case |
| G3-T63 | List by RG and by subscription | `GET servers` (RG) and `GET /subscriptions/{sub}/providers/Microsoft.Sql/servers` => 200, subscription list spans two RGs | T1/T2 | Engine: `Under` subscription-wide list. Missing in miniblue |
| G3-T64 | Delete semantics | `DELETE` existing => 200; `DELETE` missing => 204; `GET` after => 404 `ResourceNotFound` | T1/T2 | Spec lists 200/202/204; engine is sync, never 202. miniblue returns 202/404 today |
| G3-T65 | Deleting a server removes its databases and singleton children | `DELETE servers/{s}` then `GET` db, firewall rule, `connectionPolicies/default` => 404 | T2 | Needs `DeleteTree`; today only databases are removed (by prefix) |
| G3-T66 | FQDN is `{name}.database.<host>`; `location` is required | `GET` => `fullyQualifiedDomainName` ends with the emulator database suffix, not `localhost`; `PUT` without `location` => 400 (code `(verify)`) | T2/T3 | Real suffix is `database.windows.net`. miniblue defaults a missing location to `eastus` |
| G3-T67 | `azurerm_mssql_server` applies, reads back and destroys | `terraform apply` then `plan` => empty plan; `destroy` succeeds | T3 | New case `azurerm/sql`. Read path touches more singleton children than the singleton row lists `(verify against provider source)` |
| G3-T68 | An update PUT that omits `administratorLogin` keeps the stored login | `PUT` (update) without `administratorLogin` => 200, login unchanged | T2 | Login is immutable after create. Whether omitted tags are kept or cleared on PUT is `(verify)`: ARM PUT is normally a full replace |

### Databases

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T69 | Create and update a database with service defaults | `PUT servers/{s}/databases/{d}` => 201 then 200; `type` `Microsoft.Sql/servers/databases`; `collation` `SQL_Latin1_General_CP1_CI_AS`, `status` `Online`, `maxSizeBytes` 2147483648, `zoneRedundant` false, `readScale` `Disabled`, `requestedBackupStorageRedundancy` `Geo`, `currentSkuName`, `databaseId` GUID, `creationDate`; `location` taken from the server | T2 | Default values are `(verify)`; they depend on the sku |
| G3-T70 | Database under a missing server fails | `PUT` under unknown server => 404 `ResourceNotFound` | T1/T2 | Engine parent-exists default may emit `ParentResourceNotFound`; pick one in `Sample` `(verify)` |
| G3-T71 | Database tags persist on PUT and PATCH | `PUT`/`PATCH` with tags => 200, `GET` returns them | T2 | miniblue drops tags |
| G3-T72 | List, delete, GET after delete | `GET databases` => 200; `DELETE` existing => 200; `GET` after => 404; `DELETE` missing => 204 | T1/T2 | Spec lists 200/202/204 |
| G3-T73 | `sku` and `properties` round-trip | `PUT` with `sku.name` and `properties.maxSizeBytes` => `GET` returns the same values | T3 | `azurerm_mssql_database` sends `sku.name` and a size in GB. Add to `azurerm/sql` |

### Singleton children

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T75 | GET returns defaults with no prior PUT, and PUT round-trips | `GET connectionPolicies/default` => `connectionType` `Default`; `GET databases/{d}/transparentDataEncryption/current` => `state` `Enabled`; `securityAlertPolicies/default`, `backupShortTermRetentionPolicies/default`, `backupLongTermRetentionPolicies/default`, `sqlVulnerabilityAssessments/default` => 200; `PUT` then `GET` => same body | T2/T3 | Default values are `(verify)`. Engine-gap: singleton |

### Firewall rules

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T76 | Firewall rule lifecycle | `PUT servers/{s}/firewallRules/{r}` with `startIpAddress`/`endIpAddress` => 201 then 200; `GET` => same; `GET list` => 200; `DELETE` => 200, missing => 204; `GET` missing => 404 `ResourceNotFound`; start greater than end => 400 (code `(verify)`); a rule `0.0.0.0` to `0.0.0.0` is accepted (allow Azure services) | T2/T3 | Spec: PUT 200/201 and DELETE 200/204, both sync. Rule name `AllowAllWindowsIPs` `(verify)`. azurerm does a GET first on create, so a missing rule must be a clean 404. `azurerm_mssql_firewall_rule` |

## Pitfalls

- Never return `administratorLoginPassword` (spec: create/update only). Strip it before the document is stored, not only on read, so dumps and logs stay clean.
- DELETE of a missing server or database is 204, not 404. The spec lists no 404 for DELETE.
- Real Azure may answer SQL writes and deletes with 202 plus a poll URL (long-running). The engine is sync: 201 or 200 on write, 200 or 204 on delete, no `Location`.
- `fullyQualifiedDomainName` must be a real-looking host, not `localhost`; azurerm and clients build connection strings from it.
- Server names are global across subscriptions, so the uniqueness check cannot be scoped to the RG.
- A password supplied on a later PUT must not make the response differ from a GET.

## Dropped

| ID | Reason |
|---|---|
| G3-T74 | Low value; whether the real list includes the system `master` database is `(verify)` and no client in scope depends on it |
| G3-T77 | Backlog of unserved types (virtualNetworkRules, elasticPools, AAD administrators, auditingSettings, failoverGroups); add scenarios only when a TF case needs one |
