# App Configuration

> Engine fit: hooks plus engine gaps for the ARM types; data plane bespoke. miniblue today: hand-written service `internal/services/appconfig` (chi, no engine). Suggested wave: wave 4 (appconfig ARM, [ADR-0007](../adr/0007-migration-strategy.md)); SPIKE rank 9 ([SPIKE](../SPIKE.md)).

Real latest stable api-version is 2024-06-01 for the management plane (spec listing, confirmed). Per [ADR-0006](../adr/0006-sync-lro-and-errors.md) the engine is sync only: the real PUT, PATCH, DELETE and purge are long-running operations, but the engine answers 200/201/204 with `provisioningState` `Succeeded` and never sends `Azure-AsyncOperation`. SDK pollers accept that. Scenarios below assert the sync shape.

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.AppConfiguration/configurationStores` | 2024-06-01 (stable list: 2019-10-01 to 2024-06-01) | RG; list by RG and by subscription | replicas, snapshots, keyValues, private endpoint connections | `listKeys`, `regenerateKey`; PATCH; soft-delete on DELETE | hooks + gaps | `Hooks{Validate, Defaults}`, `CheckName`, `PatchMode` merge, `DefaultIdentity`, `Type.Actions` (lands with this first listKeys consumer), sub-scope list, soft-delete seam (no ADR yet) | H | M |
| `.../configurationStores/replicas` | 2024-06-01 | child of store | none | none | hooks + gaps | `ChildID`, parent check (engine built-in), SKU check in `Validate` | M | S |
| `.../configurationStores/snapshots` (ARM) | 2024-06-01 | child of store | none | none | hooks + gaps | `ChildID`, parent check, `Validate` | L | S |
| `.../configurationStores/keyValues` (ARM) | 2024-06-01 | child of store | none | none | passthrough | `ChildID`, parent check. Not in the source inventory; see `G1-N3` | L | S |
| `Microsoft.AppConfiguration/locations/deletedConfigurationStores` | 2024-06-01 | subscription + location | none | POST `.../{name}/purge` | engine gap | soft-delete seam (no ADR yet), `Type.Actions`, sub-scope list | M | M |
| `Microsoft.AppConfiguration/checkNameAvailability` and `locations/{loc}/checkNameAvailability` | 2024-06-01 | subscription | none | POST | bespoke (provider-level chi route, [ADR-0001](../adr/0001-single-arm-pipeline.md)) | none; `CheckName` may be shared for the regex | L | S |
| Data plane: `/kv`, `/labels`, `/revisions`, `/keys`, `/locks/{key}`, `/snapshots`, `/operations` | 1.0, 2023-10-01, 2023-11-01, 2024-09-01, 2026-04-01 (data-plane stable listing) | per-store host `{name}.azconfig.io` | kv, labels, revisions, snapshots | lock and unlock | bespoke (data planes never migrate) | none | H | M |

## miniblue gaps

Ordered by value.

- No soft-delete: DELETE hard-removes the store and there is no `deletedConfigurationStores` or purge (`internal/services/appconfig/handler.go:231`).
- DELETE returns 202 (`handler.go:240`). The engine answers 200 or 204, never 202.
- No PATCH, no `regenerateKey`, no list-by-subscription.
- `listKeys` returns two static keys, `miniblue-primary-key` and `miniblue-secondary-key` (`handler.go:263-296`), not the real four.
- `GET .../replicas` always returns an empty list (`handler.go:250`); no replica PUT, GET or DELETE; no snapshots; no ARM keyValues.
- PUT defaults a missing `location` to `eastus` (`handler.go:146-148`) and a missing `sku` to `free` (`handler.go:152`). Real Azure requires both.
- `creationDate` is hard-coded to `2026-01-01T00:00:00Z` (`handler.go:164`); it must be set once on create and stay stable.
- No SKU-dependent defaults (`softDeleteRetentionInDays`, `enablePurgeProtection`, revision retention).
- Data plane: the ETag is `"etag-" + key` (`handler.go:102`), so it does not change when the value changes. `getKeyValue` reads the stored value with a struct type-assert (`handler.go:119`), the persistence-roundtrip bug class from the spike findings.
- Data plane: DELETE always returns 204 (`handler.go:130`); no `/keys`, `/labels`, `/revisions`, lock, snapshots or null-label (`%00`) handling.
- Existing tests: `tests/appconfig_test.go`, `tests/appconfigarm_test.go`. No azurerm or azapi fixture for `azurerm_app_configuration`.
- `website/docs/services/parity.md:59` says "No labels, no feature flags". Labels work through the `label` query parameter, so the line is stale.

## Scenarios

### configurationStores

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T1 | Create a Free-SKU store | `PUT .../configurationStores/{n}` => 201; `id`, `name`, `type` (canonical `Microsoft.AppConfiguration/configurationStores`), `location`, `sku.name` echoed; `properties.provisioningState` `Succeeded`; `properties.endpoint` host `{n}.azconfig.io`; `creationDate` set | T2 | Endpoint suffix differs in sovereign clouds `(verify)`. `creationDate` is read-only. |
| G1-T2 | Property defaults on create | `publicNetworkAccess` `Enabled`, `disableLocalAuth` false; non-Free: `softDeleteRetentionInDays` 7, `enablePurgeProtection` false; Free: both absent | T1/T2 | `Defaults` hook. Spec defaults 7 and false are confirmed; that Free omits both, `publicNetworkAccess` `Enabled` on GET, and `createMode` `Default` being echoed are `(verify)`. Also `defaultKeyValueRevisionRetentionPeriodInSeconds`: 604800 Free, 2592000 Standard and Premium (spec, confirmed). |
| G1-T3 | PUT the same name again | 200; `id` unchanged; `creationDate` unchanged | T1/T2 | Replace, not create. |
| G1-T4 | PUT without `location` | 400; `sku` omitted is also 400 (spec marks both required) | T2 | Error code `(verify)`. Today defaults to `eastus` and `free`. |
| G1-T5 | GET existing; GET missing | `GET .../{n}` => 200; missing => 404 `ResourceNotFound` | T1/T2 | |
| G1-T6 | List by RG and by subscription | `GET .../resourceGroups/{rg}/providers/Microsoft.AppConfiguration/configurationStores` and `GET /subscriptions/{s}/providers/Microsoft.AppConfiguration/configurationStores` => 200 `{value:[...]}` containing the store | T2 | Needs sub-scope list. Spec list responses may carry `nextLink`; omit when single page. |
| G1-T7 | PATCH tags and sku | `PATCH .../{n}` => 200; supplied fields change; untouched properties preserved | T1/T2 | `PatchMode` merge. Sku downgrade rules `(verify)`. |
| G1-T8 | PATCH `enablePurgeProtection` true when unset (non-Free) | 200; GET shows `true` | T2 | |
| G1-T9 | PATCH `enablePurgeProtection` false after it was enabled | 400 | T2 | Purge protection is irreversible. Error code `(verify)`. |
| G1-T10 | Enable purge protection on a Free-SKU store | 400 | T2 | Free has no soft delete, so purge protection is invalid. Error code `(verify)`. |
| G1-T11 | DELETE then GET | `DELETE .../{n}` => 200 or 204; `GET` => 404 `ResourceNotFound`; for a non-Free store the name appears in `deletedConfigurationStores` | T2 | Real DELETE is a long-running operation (200/202/204 in spec); engine answers sync. Free store goes to the deleted list `(verify)`. |
| G1-T12 | `listKeys` | `POST .../{n}/listKeys` => 200; four entries named `Primary`, `Secondary`, `Primary Read Only`, `Secondary Read Only`; each has `id`, `name`, `value`, `connectionString` shaped `Endpoint=https://{n}.azconfig.io;Id={id};Secret={value}`, `readOnly`, `lastModified` | T2/T4 | Field set confirmed against the spec. 32-byte base64 `value` and exact `name` strings `(verify)`. Needs `Type.Actions`. azurerm reads keys after create `(verify)`. |
| G1-T13 | `regenerateKey` for one key | `POST .../{n}/regenerateKey` body `{id}` => 200 with the updated key; only that key's `value`, `connectionString` and `lastModified` change | T2 | Spec returns an `ApiKey` body, sync 200. |
| G1-T14 | `regenerateKey` with an unknown `id` | 404 | T2 | Error code `(verify)`; 400 is also plausible. |

### deletedConfigurationStores

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T15 | Read and list soft-deleted stores | `GET /subscriptions/{s}/providers/Microsoft.AppConfiguration/locations/{loc}/deletedConfigurationStores/{n}` => 200 with `configurationStoreId`, `location`, `deletionDate`, `scheduledPurgeDate`, `tags`, `purgeProtectionEnabled`; `GET .../deletedConfigurationStores` (subscription list) => 200; unknown name => 404 | T2 | Spec has the list by subscription, not by location. 404 code `(verify)`. |
| G1-T16 | Purge a store that has purge protection | `POST .../deletedConfigurationStores/{n}/purge` => 409; the entry remains | T2 | Error code `Conflict` `(verify)`. |
| G1-T17 | Purge otherwise | `POST .../purge` => 200 or 204; the entry is gone; the same name can be created again | T2 | Spec: 200/202/204 long-running; engine answers sync. |

### replicas

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T18 | Create a replica on a Standard store | `PUT .../{n}/replicas/{r}` => 201; `type` `Microsoft.AppConfiguration/configurationStores/replicas`; `properties.endpoint` `https://{n}-{r}.azconfig.io`; `provisioningState` `Succeeded` | T2 | Endpoint shape `(verify)`. Spec returns 200 or 201, long-running. |
| G1-T19 | Create a replica on a Free or Developer store | 400 `InvalidRequest` with a message that geo-replication is unsupported for those SKUs | T2 | Code and message `(verify)`. Developer SKU `(verify)`. |
| G1-T20 | Replica name with non-alphanumeric characters | 400 | T1/T2 | Spec has no name pattern for replicas; the rule `^[a-zA-Z0-9]*$` is `(verify)`. Use `CheckName` in `Validate`. |
| G1-T21 | GET, DELETE and list a replica; unknown replica; missing parent store | GET => 200; DELETE => 200 or 204 then GET 404; list => 200; unknown replica => 404 `ResourceNotFound`; missing store => 404 `ParentResourceNotFound` | T1/T2 | Parent and RG existence are engine built-ins ([ADR-0007](../adr/0007-migration-strategy.md)). Real code for a missing store `(verify)`. |

### snapshots

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T22 | Create and read an ARM snapshot | `PUT .../{n}/snapshots/{s}` with 1 to 3 `filters` and a retention period => 201 with `status` `Ready` `(verify)`; no filters => 400; `GET` => 200 | T2 | Low priority. Spec confirms `filters` min 1, max 3, `status` enum (`Provisioning`, `Ready`, `Archived`, `Failed`) and `compositionType` (`Key`, `Key_Label`). Retention bounds 3600 to 7776000 seconds are `(verify)`. |

### Acceptance

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T23 | `azurerm_app_configuration` (free) apply and refresh | clean plan; `endpoint` contains the store host; `sku` `free` | T3 | New fixture under `tests/terraform/azurerm/`. azurerm calls listKeys after create, and on destroy may recover or purge soft-deleted stores `(verify)`, so G1-T12, G1-T15 and G1-T17 shapes matter. |

### New scenarios

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-N1 | `new`: store name rules | `PUT` with a name shorter than 5, longer than 50, or outside `^[a-zA-Z0-9_-]*$` => 400 | T1/T2 | Spec pattern and length confirmed. `CheckName` returns `InvalidResourceName` ([ADR-0003](../adr/0003-pipeline-phases-and-hooks.md)); the real code is `(verify)`. Names are globally unique because the endpoint is a public host; a cross-subscription clash is not emulable `(verify)`. |
| G1-N2 | `new`: provider-level name check | `POST /subscriptions/{s}/providers/Microsoft.AppConfiguration/checkNameAvailability` body `{name, type}` with `type` `Microsoft.AppConfiguration/configurationStores` => 200 `{nameAvailable, reason, message}`; also under `locations/{loc}/checkNameAvailability` | T2 | Chi route, not an engine type. Shape confirmed against the spec; `reason` values `(verify)`. |
| G1-N3 | `new`: ARM key-values | `PUT .../{n}/keyValues/{key}` with `properties.value`, `contentType`, `tags` => 200; `GET`, `DELETE` round-trip | T2 | Present in the 2024-06-01 spec (PUT returns 200 only). Name encoding `key$label` `(verify)`. Parent store required. |
| G1-N4 | `new`: recover a soft-deleted store | `PUT` with `properties.createMode` `Recover` for a soft-deleted name => 200 or 201 and the entry leaves `deletedConfigurationStores`; `createMode` `Default` on a soft-deleted name => conflict | T2 | `Recover` and `Default` enum confirmed. Conflict code `(verify)`. |

### Data plane (bespoke)

Host-routed (`{name}.azconfig.io`) and path-routed (`/appconfig/{store}/...`). Tier T2 only; data planes never migrate ([ADR-0007](../adr/0007-migration-strategy.md)). Collections use `items` and `@nextLink`, not `value` `(verify)`.

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T24 | Set then get a key-value | `PUT /kv/{key}?label={l}` then `GET` => `key`, `label`, `value`, `content_type`, `etag`, `last_modified`, `tags`, `locked` false round-trip | T2 | Field set confirmed against the spec. The `etag` must be opaque and change on every write. |
| G1-T25 | Key Vault pointer content type | A value stored with content type `application/vnd.microsoft.appconfig.keyvaultref+json;charset=utf-8` reads back byte-for-byte | T2 | Applies to the feature-flag content type too. |
| G1-T26 | Labels and filters | The same key under two labels is independent; `label=%00` selects the null label; `GET /kv?key=prefix*&label=l` filters by glob and label | T2 | `%00` encoding and glob syntax `(verify)`. |
| G1-T27 | Delete then get | `DELETE /kv/{key}` => 200 with the deleted item when it existed, 204 otherwise; `GET` => 404 | T2 | Spec allows 200 or 204; the "200 with body when it existed" rule is `(verify)`. Today always 204. |
| G1-T28 | Lock and unlock | `PUT /locks/{key}` => `locked` true; `PUT` or `DELETE /kv/{key}` => 409 with body `{type, title, name, detail, status}`; `DELETE /locks/{key}` then writes succeed | T2 | Error body shape confirmed. The `type` URI `https://azconfig.io/errors/key-locked` is `(verify)`. |
| G1-T29 | Labels and revisions lists | `GET /labels` and `GET /revisions` => 200 collections reflecting prior writes (each write adds a revision) | T2 | Revision retention follows `defaultKeyValueRevisionRetentionPeriodInSeconds`. |
| G1-T30 | Snapshots (data plane) | `PUT /snapshots/{name}` => 201, `status` goes `provisioning` to `ready`; `GET /snapshots` filters by `status` and name wildcard, supports `$select`; `PATCH /snapshots/{name}` archives; `GET /kv?snapshot={name}` returns the frozen set; `snapshot` with `key` or `label` => 400 | T2 | Spec has PUT 201 and PATCH 200. Status casing and the 400 code `(verify)`. Low priority. |
| G1-N5 | `new`: key listing | `GET /keys` => 200 `items` of `{name}` for distinct keys; `HEAD` variants return headers only | T2 | Path in the data-plane spec; missing today. |

## Pitfalls

- Real PATCH, DELETE and purge are long-running operations. The engine is sync; assert 200 or 204, not 202.
- Real `location` and `sku` are required on PUT. Defaulting them hides client bugs and masks a real 400.
- `listKeys` returns four keys (two read-write, two read-only). Two static keys breaks clients that pick a read-only connection string.
- Soft-delete retention and purge protection apply to non-Free SKUs only; Free stores have neither. Purging a protected store is 409, and purge protection cannot be turned off.
- The data-plane DELETE answers 200 or 204, and an ETag that does not change on write breaks conditional requests.
- A replica endpoint is derived from the store and replica names; replicas are not available on Free or Developer.
- Naive store error codes (`StoreNotFound`, `ReplicaNotFound`) would diverge; real Azure uses `ResourceNotFound` and `ParentResourceNotFound`.

## Dropped

| ID | Reason |
|---|---|
| G1-T31 | miniblue data planes are unauthenticated by design; HMAC signature checking is out of scope unless auth emulation is added. |
| G1-T32 | Event Grid wiring for key-value and snapshot events; not portable. |
