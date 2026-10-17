# Storage

> Engine fit: bespoke for accounts and the data plane; hooks plus gaps (global-unique name, merge PATCH, post-actions) if the ARM shell migrates; passthrough child for ARM queues and tables once the account parent exists. miniblue today: hand-written service `internal/services/storageaccounts` (ARM shell) plus the Blob/Queue/Table data planes. Suggested wave: wave 4 (PLAN phase 4, `storageaccounts (ARM)`), optional; the data plane never migrates.

The account stays bespoke because blob shared-key authentication reads the account record directly, and because account PUT must keep returning 200 (SPIKE.md, storage ARM row). The engine's create path returns 201 then 200 (ADR-0005), so a migration would change the status code. The ARM shell is migrated only if a fidelity or enforcement need appears (ADR-0007, wave 4). `Type.Actions` lands with the first `listKeys` consumer; storage is a natural first one.

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.Storage/storageAccounts` | spec repo: 2023-05-01, 2024-01-01, 2025-01-01, 2025-06-01, 2025-08-01, 2026-04-01, 2026-06-01, 2026-09-01 (latest `(verify)`); existing code cites the 2025-01-01 schema | RG; list by subscription and by RG | blobServices, queueServices, tableServices, fileServices | `POST listKeys`, `regenerateKey`, `ListAccountSas`, `ListServiceSas`, `revokeUserDelegationKeys`; `PATCH`; also migration actions (out of scope) | bespoke; hooks plus gaps if migrated | `CheckName`, global-unique name, `PatchMode` merge with typed properties, `Type.Actions`, RG existence, `Provisioned` | H | M |
| `Microsoft.Storage/checkNameAvailability` (subscription provider action) | same | subscription | none | `POST /subscriptions/{s}/providers/Microsoft.Storage/checkNameAvailability` (200) | bespoke (route, not a type) | name store shared with the account PUT | M | S |
| `storageAccounts/blobServices/default` and `.../containers/{c}` | same | account | containers | service props `GET`/`PUT`/`PATCH`; containers list | singleton plus passthrough child | parent existence | M | S |
| `storageAccounts/queueServices/default/queues/{q}` | same | account | none | none | passthrough child under a singleton parent | `ChildID`, parent existence | M | S |
| `storageAccounts/tableServices/default/tables/{t}` | same | account | none | none | passthrough child under a singleton parent | `ChildID`, parent existence | M | S |
| `storageAccounts/fileServices/default` | same | account | shares (not served) | none | singleton | none | L | S |
| Data planes: Blob, Queue, Table, SAS validators | n/a | account endpoint | none | SAS HMAC-SHA256 (account, service, user-delegation, bearer) | bespoke | none; never migrates | M | M to L |

Real facts confirmed in the REST spec (api-version 2025-01-01):

- Account name is 3 to 24 characters, `^[a-z0-9]+$` (lowercase letters and digits only).
- PUT returns 200 or 202 (long-running); DELETE 200 or 204; PATCH 200; `listKeys`, `regenerateKey`, `ListAccountSas`, `ListServiceSas` and `checkNameAvailability` return 200.
- `checkNameAvailability` takes `{name, type}` with `type` `Microsoft.Storage/storageAccounts`; `reason` is `AccountNameInvalid` or `AlreadyExists`.

## miniblue gaps

Ordered by value.

- **No account PATCH.** Terraform and azapi update tags and properties with PATCH or a full PUT; the PATCH route is missing.
- **PUT rebuilds the whole record.** `creationTime` and `keyCreationTime` are regenerated on every PUT, and PUT always returns 200 (never 201) (`buildStorageAccountResponse`).
- **Unknown properties are dropped.** The body `properties` are filtered through an allow-list of about 24 keys; anything else is not echoed back. azapi sends the properties it was given and treats a missing echo as drift.
- **Keys are derived, not stored.** Keys come deterministically from subscription, resource group and account name, so there is no `regenerateKey` and a rotation cannot persist.
- **No `checkNameAvailability`, no SAS list actions** (`ListAccountSas`, `ListServiceSas`), and no `revokeUserDelegationKeys`.
- **No validation, no uniqueness, no RG check.** No name rules on PUT, no global name uniqueness, and no resource group existence check.
- **No ARM `queues/{q}` or `tables/{t}` children.** Only the `queueServices/default` and `tableServices/default` property endpoints exist.
- `etag` is a static `W/"miniblue"`. Real Azure returns a changing etag; clients rarely rely on it.
- Endpoints honour `MINIBLUE_STORAGE_ENDPOINT` when set.
- ARM container CRUD and list already exist, so the container delta is small.
- DELETE returns 200 when the account existed and 204 when it did not, and removes `blob:armcontainer`, `managementpolicy` and `svcprops` records.
- Existing coverage: `tests/blobarm_test.go` (account CRUD, list, `listKeys`, container CRUD, endpoint formats, shared-key data plane, queue/table/file/blob service props), `internal/services/storageaccounts/{cleanup,delete,management_policy,service_properties}_test.go`, and the azurerm fixture `tests/terraform/azurerm/storage/main.tf` (account plus container through `storage_account_id`). There is no azapi storage fixture (the azapi directory holds only `policy_definition`).

## Scenarios

### Storage accounts

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T38 | PUT an account. The body carries `primaryEndpoints` for blob, queue, table, file, web and dfs, `provisioningState=Succeeded`, `creationTime`, `sku` and `kind`; `secondaryEndpoints` appear only for read-access geo-redundant SKUs. | PUT `.../storageAccounts/{n}` => 200 (spec 200/202; sync), body per left | T2/T3 | Real create is long-running (202); stay sync 200, not 201 (spec lists 200 and 202). RA-GRS and RA-GZRS list is `(verify)`. Endpoint hosts follow `MINIBLUE_STORAGE_ENDPOINT`. |
| G3-T39 | PUT on an existing account keeps both keys and `creationTime`. PATCH `tags` changes the tag map and keeps the keys. PATCH on a missing account fails. | PUT => 200, keys and `creationTime` unchanged; PATCH => 200; missing => 404 `ResourceNotFound` | T2 | Today PUT regenerates `creationTime`. Tag PATCH replaces the map `(verify)`. Needs `PatchMode` merge. |
| G3-T40 | PATCH merges `accessTier`, `allowBlobPublicAccess`, `minimumTlsVersion`, `supportsHttpsTrafficOnly` and `networkAcls`. Properties the emulator does not model survive a round trip. | PATCH => 200; GET returns merged values plus the unknown property | T2/T3 | azapi fixture. Nested merge semantics for `networkAcls` `(verify)`. Server-owned fields (keys, `creationTime`) must survive the merge. |
| G3-T41 | `listKeys` returns `key1` and `key2` (base64, 64 bytes), with `permissions` `FULL`, stable between calls. Always two keys. | POST `.../listKeys` => 200, `keys[2]` each `{keyName, value, permissions}` | T2/T4 | `permissions` casing `(verify)` (`FULL` vs `Full`). Stability assertion is the delta. First `Type.Actions` consumer. |
| G3-T42 | `regenerateKey` for `key1` changes `key1` only; `keyName` is case-insensitive. An unknown key name fails. | POST `.../regenerateKey` body `{keyName}` => 200 with both keys; unknown => 400 `(verify)` | T2 | Needs stored key state (today derived), read by the shared-key data plane. Real status for an unknown key (400 vs 404) is `(verify)`. |
| G3-T43 | Check name availability. A valid free name is available; uppercase, shorter than 3 or longer than 24 characters is `AccountNameInvalid`; a taken name is `AlreadyExists`. | POST `/subscriptions/{s}/providers/Microsoft.Storage/checkNameAvailability` => 200, `{nameAvailable, reason, message}` | T2 | Missing today. `reason` values confirmed in the spec; `message` text `(verify)`. Not an engine type, a chi route sharing the name store. |
| G3-T44 | A duplicate account name in another resource group or subscription fails; deleting the account frees the name. | PUT => 409 `StorageAccountAlreadyTaken` `(verify)` | T2 | Names are global across Azure. Global-unique store needs the engine gap (global-unique). |
| G3-T45 | Name rules are enforced on PUT: 3 to 24 characters, lowercase letters and digits only. | PUT with a bad name => 400 `AccountNameInvalid` `(verify)` | T1/T2 | Same rule set as `CheckName`. The path pattern and length are confirmed in the spec; the error code on PUT is `(verify)`. |
| G3-T46 | DELETE an existing account then a missing one. GET after delete fails. An empty resource group lists nothing. List by subscription spans resource groups. | DELETE existing => 200; missing => 204; GET => 404 `ResourceNotFound`; list RG => 200 `{"value":[]}`; list subscription => 200 with accounts from several RGs | T2 | Spec lists 200 and 204. Covered in part by `tests/blobarm_test.go`; the subscription-wide list and empty-RG list are the delta. |
| G3-T47 | PUT an account in a resource group that does not exist fails. | PUT => 404 `ResourceGroupNotFound` | T1/T2 | Parent existence check (ADR-0003 phase 5). Missing today. |
| G3-T54 | Terraform: an azurerm account is created and the blob endpoint contains the account name; `minimum_tls_version` round-trips. An azapi `Microsoft.Storage/storageAccounts` fixture does the same with a raw body. | `terraform apply` succeeds; second plan is empty | T3 | azurerm fixture exists. Add the azapi `storage_account_basic` fixture; it exercises the unknown-property echo from the PATCH merge scenario. Ensure no plan drift. |

### Blob containers (ARM)

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T48 | PUT a container twice (idempotent), GET returns its name, GET a missing one fails, the ARM list includes it, DELETE removes it. | PUT `.../blobServices/default/containers/{c}` => 201 or 200 `(verify)`; GET => 200 or 404; list => 200; DELETE => 200 or 204 `(verify)` | T2 | Mostly covered by `tests/blobarm_test.go`; delta is the list shape and the status codes. Parent account missing => 404 `ResourceNotFound`. |

### Queues and tables (ARM)

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T49 | ARM queue: PUT is idempotent, GET returns it, GET a missing queue fails, DELETE removes it. PUT under a missing account fails. | PUT `.../queueServices/default/queues/{q}` => 200 or 201 `(verify)`; GET => 200 or 404; DELETE => 200 or 204 `(verify)`; missing account => 404 `ResourceNotFound` | T2 | Missing today. Decide whether an ARM-created queue is visible to the Queue data plane; real Azure shares one namespace. |
| G3-T50 | ARM table: same shape as the queue scenario. | PUT `.../tableServices/default/tables/{t}` and the same status set | T2 | Missing today. Table name rules (letters and digits, 3 to 63, starting with a letter) `(verify)`. |

### Data plane (bespoke)

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T51 | Account SAS (`ListAccountSas`) and service SAS (`ListServiceSas`) tokens carry `sv` and `sig`. Using one on a blob read works. An expired token or a token without the needed permission is refused. | POST `.../ListAccountSas` and `.../ListServiceSas` => 200 with a token; blob GET with the token => 200; expired => 403 `AuthenticationFailed`; wrong permission => 403 `(verify)` code | T2 | Bespoke. SAS signing is HMAC-SHA256 over the stored key, signed version `(verify)`. The two control-plane POST actions are the missing piece. |
| G3-T53 | Table entity upsert and get by partition key and row key (`pk1`, `rk1`). | PUT or MERGE entity => 204; GET entity => 200 with the stored properties | T2 | Bespoke. The Table data plane is thin today. Optional follow-up: an `azurerm_storage_table_entity` fixture. |

## Pitfalls

- Account create is long-running in real Azure: the spec lists 200 or 202 for PUT and no 201. A synchronous emulator returns 200 with `provisioningState: Succeeded`, which pollers treat as done (ADR-0006).
- Account names are globally unique across all of Azure and fixed to 3 to 24 lowercase letters and digits. Uppercase is rejected, not lower-cased.
- A PUT on an existing account is an update: `creationTime`, `keyCreationTime` and both keys must not change. Rebuilding the record on every PUT makes Terraform plans drift.
- Do not drop properties the emulator does not model. azapi and Bicep send the full body and compare the response; a filtered echo causes a perpetual diff.
- Keys need real state. If keys are derived from the name, `regenerateKey` cannot change them and the shared-key data plane cannot honour a rotation.
- `listKeys` is a POST action, not a GET, and always returns two keys.
- DELETE on a missing account is 204 (spec 200 or 204), not 404. This differs from resource groups.
- `checkNameAvailability` is a subscription-level provider action, not an account sub-route; it must agree with what a later PUT accepts.
- ARM queues and tables are separate from data-plane queues and tables in the service model, but one namespace in real Azure. Decide the shared store explicitly.
- Shared-key signatures are over the stored key. A SAS minted from `ListAccountSas` must validate against the same key the data plane uses.

## Dropped

| ID | Reason |
|---|---|
| G3-T52 | User-delegation SAS and bearer-auth blob access need Entra token validation and a user delegation key; data plane, low value, out of scope for the ARM foundation. |
