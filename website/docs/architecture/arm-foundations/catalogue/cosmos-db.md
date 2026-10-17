# Cosmos DB

> Engine fit: hooks for `Microsoft.DocumentDB/databaseAccounts` and its SQL children (defaults, derived locations, parent checks), engine-gap for key actions (`Type.Actions`), the name-check HEAD and the create status, bespoke for the data plane and the SQL query engine. miniblue today: hand-written service `internal/services/cosmosdb` (ARM plus a miniblue-specific data plane). Suggested wave: PLAN wave 4 (ARM only; data planes never migrate).

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.DocumentDB/databaseAccounts` | 2024-08-15, 2024-11-15, 2025-04-15, 2025-10-15, 2026-03-15 (latest in spec) | RG; list by RG and by subscription | `sqlDatabases`, `tables`; other APIs (Mongo, Cassandra, Gremlin) out of scope | PATCH; POST `listKeys`, `listConnectionStrings`, `regenerateKey`; GET and POST `readonlykeys` | hooks + engine-gap | `Hooks.Defaults` (locations, backup, access defaults), `Hooks.Validate` (required properties, name), `PatchMode` merge, `Type.Actions`, per-type create-status override | H | M |
| `Microsoft.DocumentDB/databaseAccountNames/{name}` | same | provider level, no subscription or RG | none | HEAD | engine-gap | `CheckName` does not obviously cover a HEAD with no body `(verify)`; otherwise a chi route | L | S |
| `databaseAccounts/sqlDatabases` | same | under an account | `containers`, `throughputSettings/default` | none | hooks + parent check | `Hooks.Defaults`, `ChildID` for the fixed `default` child `(verify)` | H | S |
| `sqlDatabases/containers` | same | under a database | `throughputSettings/default` | none | hooks + parent check | `Hooks.Defaults` (partition key, indexing defaults) | H | S |
| `throughputSettings/default` (database and container) | same | singleton child | none | GET, PUT; POST `migrateToAutoscale`, `migrateToManualThroughput` | hooks + parent check | `ChildID` `(verify)`, `Type.Actions` for the migrate POSTs | M | S |
| Data plane (documents, collections, databases, pkranges) | n/a | account endpoint host | none | CRUD, query, PATCH | bespoke | none | H | M |
| SQL query engine | n/a | data plane | none | POST query | bespoke | none | H | L |

## miniblue gaps

- Container PUT (`internal/services/cosmosdb/handler.go:452-479`) ignores the body and hard-codes `partitionKey.paths` to `["/id"]` (`handler.go:469`). An azurerm container with `/pk` drifts on every plan. Existing bug.
- `CreateOrUpdateSQLDatabase` (`handler.go:389-409`) ignores the body and does no parent-account check. Database and container PUT return 200, DELETE returns 202 (`handler.go:435`, `handler.go:507`) or 404 for a missing resource.
- `regenerateKey` (`handler.go:376`) validates nothing and returns the same static keys (`accountKeysResponse`, `handler.go:338`), so no key ever changes.
- `throughputSettings` exists only for tables (`handler.go:54-56`, `handler.go:584-603`), not for sqlDatabases or containers.
- Missing: HEAD `databaseAccountNames`, PATCH on accounts, list by subscription, RG-existence check, echo of `disableLocalAuth`.
- Account PUT returns 200 always (`handler.go:291`), which matches the spec but conflicts with the ADR-0006 "201 on create" rule. Account DELETE returns 202 (`handler.go:317`).
- `documentEndpoint` is fixed to `https://{name}.documents.azure.com:443/` (`handler.go:239`).
- Data plane is served at a miniblue path, `/cosmosdb/{account}/dbs/{db}/colls/{coll}/docs` (`handler.go:64`), not on the account endpoint host. `ReplaceDocument` (`handler.go:118`) upserts instead of returning 404 and can panic on a nil document; document DELETE returns 204 even when missing (`handler.go:139`); `QueryDocuments` (`handler.go:142`) returns every document with no SQL; there is no `If-Match` / 412, no PATCH, no `/dbs` or `/colls` CRUD, no pkranges.
- Existing coverage: `tests/cosmosdb_test.go` (3 document tests), `tests/cosmosdbarm_test.go` (12 ARM tests), `internal/services/cosmosdb/handler_test.go`. No azurerm Cosmos fixture.

## Scenarios

### databaseAccounts

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T98 | PUT an account: `kind=GlobalDocumentDB`, `Session` consistency, `databaseAccountOfferType=Standard`, one location | PUT => 200 (spec: 200 only, LRO; no 201); `kind`, `databaseAccountOfferType=Standard`, `consistencyPolicy`, `documentEndpoint=https://{name}.documents.azure.com:443/`, `provisioningState=Succeeded` | T2 | Spec requires `properties.locations` and `databaseAccountOfferType`; error code on omission `(verify)`. Name is 3-50 characters, `^[a-z0-9]+(-[a-z0-9]+)*`. Needs a per-type create-status override. The endpoint host should follow miniblue's configured host `(verify)` |
| G1-T99 | Derived fields on a created account | `writeLocations` and `readLocations` computed from `locations` (failoverPriority 0 is the write region), each with `id`, `locationName`, `documentEndpoint`, `provisioningState`; `failoverPolicies` derived; `backupPolicy` Periodic with 240 minutes interval and 8 hours retention; `publicNetworkAccess=Enabled` | T2 | `Hooks.Defaults`. `backupStorageRedundancy` default `(verify)`. Property list from the spec: `readLocations`, `writeLocations`, `failoverPolicies`, `backupPolicy`, `publicNetworkAccess` |
| G1-T100 | `disableLocalAuth` default and explicit true | Default reads back `false`; `true` is stored and echoed on GET | T2 | Property exists in the spec. Not echoed by miniblue today. Behaviour of the data plane when true is dropped (see Dropped) |
| G1-T101 | PATCH tags, GET, DELETE, GET again | PATCH => 200 with new tags and other properties intact; GET => 200; DELETE => 200 or 204 in the sync engine (spec 202/204); GET after => 404 `ResourceNotFound` `(verify)` | T2 | `PatchMode` merge. The account-specific code `DatabaseAccountNotFound` is not an ARM code; exact real code for a missing account `(verify)` |
| G1-T102 | List by RG and by subscription | Both 200 and contain the account | T1 | Sub-wide list is engine behaviour (ADR-0003) |
| G1-T103 | POST `listKeys`, POST/GET `readonlykeys`, POST `listConnectionStrings`, POST `regenerateKey` with `keyKind`; an invalid `keyKind` | `listKeys` => `primaryMasterKey`, `secondaryMasterKey`; `readonlykeys` => `primaryReadonlyMasterKey`, `secondaryReadonlyMasterKey`; `listConnectionStrings` => `connectionStrings[]`; `regenerateKey` (spec 200/202; 200 in the sync engine) changes only the chosen key; invalid `keyKind` => 400 `InvalidKeyKind` `(verify)` | T2 | `Type.Actions` lands with the first consumer of this scenario. `keyKind` values: `primary`, `secondary`, `primaryReadonly`, `secondaryReadonly` (spec enum). Key format is base64 `(verify)`. Stable between calls until regenerated |
| G1-T104 | HEAD `/providers/Microsoft.DocumentDB/databaseAccountNames/{name}` for a free and a used name | Free => 404; used => 200; no body either way | T2 | Spec: HEAD returns 200 or 404 only. The reason string `AlreadyExists` does not apply to a HEAD |

### sqlDatabases

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T105 | PUT, GET, list and DELETE a SQL database; PUT under a missing account; GET a missing database | PUT => 200 (spec 200/202) echoing `properties.resource.id`; GET 200; list contains it; DELETE => 200 or 204 (spec 202/204); missing account => 404 `ParentResourceNotFound`; missing database => 404 `ResourceNotFound` | T1/T2 | Parent and not-found rows are generic (T1); the body echo is T2. Spec requires `properties.resource`. Service-specific `*NotFound` codes are not ARM codes |
| G1-T106 | GET and PUT `sqlDatabases/{db}/throughputSettings/default` | GET => 200 with `properties.resource.throughput` or `autoscaleSettings`; PUT (spec 200/202) sets it; a later GET shows the new value | T2 | PUT needs `throughput` or `autoscaleSettings`, not both. GET for a database with no dedicated throughput is 404 in real Azure `(verify)`. Minimum values `(verify)` |

### sqlDatabases/containers

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T107 | PUT a container with `partitionKey.paths=["/pk"]`; GET, list, DELETE; missing container; missing database | PUT => 200 (spec 200/202) echoing `partitionKey.paths`, `kind` (default `Hash`) and `version` (1 or 2); GET and list match; DELETE => 200 or 204; missing container => 404 `ResourceNotFound`; missing database => 404 `ParentResourceNotFound` | T2 | Spec requires `resource.id`. Echo must come from the request body, not a constant. Default `indexingPolicy` shape `(verify)` |
| G1-T108 | GET and PUT `containers/{c}/throughputSettings/default` | As the database throughput row, on the container; a container created with throughput 400 reads back 400, and a PUT with a new value is reflected | T2 | Dedicated container throughput only. Migrate POSTs are not covered by the source `(verify)` |
| G1-T110 | `azurerm_cosmosdb_account` + `azurerm_cosmosdb_sql_database` + `azurerm_cosmosdb_sql_container` (partition key `/pk`, throughput 400) | apply then refresh => clean plan; endpoint is `https://{name}.documents.*`; throughput reads back 400 | T3 | New fixture `tests/terraform/azurerm/cosmosdb`. Needs the account, key, database and container rows first. Provider read calls (keys, connection strings, throughput) `(verify)` |

### Data plane (bespoke)

The data plane keeps its own routes and never migrates to the engine (ADR-0007). Real clients talk to the account endpoint host and authenticate with a signed master key or a bearer token; miniblue exposes a path form and no auth. Every row is tier T2 only and tagged bespoke.

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T111 | GET `/` on the account endpoint | 200 with account properties: writable and readable locations and consistency policy | T2 | bespoke; shape `(verify)` |
| G1-T112 | `/dbs`: create, read, list, delete | Create => 201; read => 200; list contains it; delete => 204 then 404 | T2 | bespoke. Success codes per the Cosmos REST API docs `(verify)` |
| G1-T113 | `/colls`: create, read, replace, delete, list; cross-plane visibility | 201; 200; replace => 200; delete => 204; a database or container made through ARM is visible on the data plane, and the reverse | T2 | bespoke. Needs the ARM and data-plane stores to share state |
| G1-T114 | Documents: create; duplicate id; missing id or missing partition key; read; replace; delete; stale `If-Match` | Create => 201; duplicate => 409; missing id or partition key => 400; read => 200; replace => 200 (404 if the document is absent, never an upsert); delete => 204 then 404; stale `If-Match` => 412 `PreconditionFailed` | T2 | bespoke. Replace and delete on a missing document are 404 `(verify)` |
| G1-T115 | List documents; PATCH operations | List contains the created items; a PATCH with an operation changes only the targeted field | T2 | bespoke. Operation set (`set`, `replace`, `add`, `remove`, `incr`) `(verify)` |
| G1-T116 | SQL query: `SELECT *`, `WHERE` with equality, `>`, `IN`, `IS_DEFINED`, parameterised queries | Only matching documents returned | T2 | bespoke; needs a SQL parser. Query is a POST with the query content type and `x-ms-documentdb-isquery` `(verify)` |
| G1-T117 | SQL query: field projection, `SELECT VALUE`, `ORDER BY` ascending and descending, `COUNT` | Results have the requested shape and order | T2 | bespoke |
| G1-T118 | SQL query pagination with a continuation token | Walking every page yields each document exactly once | T2 | bespoke; `x-ms-continuation` and `x-ms-max-item-count` `(verify)` |
| G1-T119 | SQL query: `GROUP BY` with `COUNT`, `SUM`, `AVG`, `MIN`, `MAX`; `ORDER BY` on an aggregate or group field; empty collection | One row per group; an empty collection yields an empty result | T2 | bespoke |
| G1-T120 | TTL expiry on documents | An expired document is no longer readable | T2 | bespoke; test the purge function as a unit. Real expiry is lazy and approximate, so assert eventual absence |
| G1-T121 | GET `.../colls/{c}/pkranges` | 200 with at least one partition key range | T2 | bespoke; SDKs call it before cross-partition queries. Response shape `(verify)` |

## Pitfalls

- Account PUT is 200 only in the spec, even on create, and the account DELETE is 202 or 204. The ADR-0006 201-on-create rule would be wrong for this type.
- Missing resources are 404 `ResourceNotFound` or `ParentResourceNotFound`. Service-specific codes (`DatabaseAccountNotFound`, `SqlDatabaseNotFound`) are not ARM codes.
- Database and container PUT bodies carry `properties.resource`. Echo it. A constant partition key makes azurerm detect drift and force a replace.
- `throughputSettings` PUT takes `throughput` or `autoscaleSettings`, never both. GET on a resource without dedicated throughput can be 404 `(verify)`.
- `listKeys` is a POST. `readonlykeys` accepts GET and POST. Static keys make `regenerateKey` untestable; the chosen key must change and the others must not.
- `regenerateKey` is a spec LRO (200/202). The sync engine returns 200.
- HEAD on `databaseAccountNames` returns no body: 404 means the name is free.
- Account `kind` can only be set at creation. PATCH merges and does not change `location`.
- Document replace is not an upsert, and DELETE on a missing document is 404.
- Data-plane auth (master-key HMAC, RBAC) is separate from ARM auth and is not modelled here.

## Dropped

| ID | Reason |
|---|---|
| G1-T109 | ARM template deployment of an account populating `locations` needs Deployments. Not portable. |
| G1-T122 | `disableLocalAuth=true` giving 401 on data-plane key auth, and RBAC-based data-plane auth. Not portable until miniblue models identities (Authorizer seam). |
