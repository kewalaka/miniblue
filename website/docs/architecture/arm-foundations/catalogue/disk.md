# Disk

> Engine fit: hooks for `Microsoft.Compute/disks` CRUD (defaults and derived `diskState`), engine-gap for the `beginGetAccess` / `endGetAccess` actions (`Type.Actions`) and the PUT create status, bespoke for the `diskOperations` poll route and the disk-SAS data plane. miniblue today: hand-written service `internal/services/disks`. Suggested wave: PLAN wave 2.

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.Compute/disks` | 2024-03-02, 2025-01-02, 2026-03-02 (latest in spec) | RG; list by RG and by subscription | none | PATCH; POST `beginGetAccess`, `endGetAccess` | hooks + engine-gap | `Hooks.Defaults` (`diskState`, `timeCreated`, `uniqueId`, access defaults, sku tier), `Hooks.Validate` (name, size), `PatchMode` merge, `Type.Actions`, per-type create-status override | M | S |
| `Microsoft.Compute/locations/diskOperations/{id}` | same | subscription + location `(verify)` | none | GET (poll) | bespoke | none (chi route); only needed if `beginGetAccess` stays asynchronous | M | S |
| Disk SAS data plane `/disk-sas/{id}` | n/a (data plane) | host/path routed by miniblue | none | HEAD, PUT range, GET range | bespoke | none (data planes never migrate, ADR-0007) | L | M |

## miniblue gaps

- No RG-existence check: `CreateOrUpdate` (`internal/services/disks/handler.go:229`) writes into any RG. Real: 404 `ResourceGroupNotFound`.
- `beginGetAccess` (`handler.go:316-327`) answers synchronously with a fake `accessSAS`. No 202 + `Location`, no `diskOperations` route, no `ActiveSAS` state, no 409 on a second grant. `endGetAccess` (`handler.go:330`) does not change `diskState`.
- No `/disk-sas` data plane (HEAD / range PUT / range GET).
- Defaults that real ARM does not apply: `location` falls back to `eastus` (`handler.go:138`). Real ARM rejects a missing `location` with 400 `LocationRequired` `(verify)`. SKU falls back to `Standard_LRS` (`handler.go:152`) `(verify against real default)`.
- PUT always returns 200 (`handler.go:248`), which matches the spec but conflicts with the ADR-0006 "201 on create" rule (see Pitfalls).
- DELETE returns 204 for a missing disk and 202 on success (`handler.go:297`, `handler.go:300`). Real allows 200/202/204; the engine would return 200 or 204 per ADR-0006.
- Existing coverage: `tests/disks_test.go` (`TestManagedDiskLifecyclePatchAccessAndLists`, `TestManagedDiskUploadStateAndResourceGroupCascade`) and `azurerm_managed_disk` in `tests/terraform/azurerm/compute/main.tf`.

## Scenarios

### disks

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T59 | PUT an Empty 32 GB `Premium_LRS` disk with a location | PUT => 200 (spec lists 200/202, never 201); body has `diskState=Unattached`, `provisioningState=Succeeded`, `timeCreated`, `uniqueId`, `publicNetworkAccess=Enabled`, `networkAccessPolicy=AllowAll`, `sku.name=Premium_LRS`, `sku.tier=Premium`, `diskSizeGB=32` | T2 | `Hooks.Defaults`. Needs a per-type create-status override vs the ADR-0006 201 rule. Access-policy defaults and `sku.tier` value `(verify)` |
| G1-T60 | PUT a disk into an RG that does not exist | PUT => 404 `ResourceGroupNotFound` | T1 | Generic engine check, no disk-specific code |
| G1-T61 | GET, list by RG, list by subscription, DELETE, GET again | GET => 200; both lists contain the disk; DELETE => 200, 202 or 204; GET after => 404 `ResourceNotFound`; DELETE of a missing disk => 204 `(verify)` | T1 | Assert the code only, not the message text. Sync engine returns 200 or 204 on delete |
| G1-T62 | PATCH tags and `diskSizeGB`; PATCH a missing disk | PATCH => 200 (spec 200/202), tags merged and `diskSizeGB` persisted; missing => 404 `ResourceNotFound` | T2 | `PatchMode` merge. Real rejects shrinking `diskSizeGB` and resizing an attached disk without deallocation `(verify)` |
| G1-T63 | POST `beginGetAccess` with `access=Read` and `durationInSeconds` | Real: 202 + `Location`, poll to a final body with `accessSAS`. Engine (sync-only): POST => 200 with `accessSAS`; then GET disk => `diskState=ActiveSAS` | T2/T4 | Engine-gap: `Type.Actions`. Sync-only ADR means no 202. Add a T4 SDK poller run to prove an SDK poller completes on a sync 200 `(verify)`. Never assert the SAS token value |
| G1-T64 | Poll the `diskOperations/{id}` URL returned by `beginGetAccess` | GET => 200 `status=InProgress` then `status=Succeeded` with `properties.output.accessSAS`; unknown id => 404 | T2 | Bespoke. Only needed if the async form is kept; conflicts with sync-only ADR-0006. Assert the terminal shape, not the in-progress timing |
| G1-T65 | `beginGetAccess` on a disk that already has an active SAS | 409 with an error code `(verify)` | T2 | Needs `diskState` tracked across calls. Code unconfirmed |
| G1-T66 | POST `endGetAccess` | Real: LRO. Engine: POST => 204 (no body); `diskState` returns to `Unattached`; the SAS URL stops working | T2 | `Type.Actions`. "SAS stops working" depends on the T67 route `(verify)` |
| G1-T68 | `azurerm_managed_disk`, `Premium_LRS`, Empty, 32 GB | `terraform apply` then refresh => clean plan; `storage_account_type` echoed | T3 | `tests/terraform/azurerm/compute` already has the resource; extend assertions |

### Data plane (bespoke)

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T67 | Disk SAS endpoint: HEAD, ranged PUT, ranged GET | HEAD => 200 with `Content-Length` = disk size; PUT with a `Range`/`x-ms-range` uploads bytes; GET with a range returns those bytes | T2 | Bespoke, low priority, stays a chi route. Exact headers real Azure uses on the SAS host `(verify)` |

## Pitfalls

- Disk PUT is 200 or 202 only in the spec; there is no 201. Generated SDK clients check the expected status set, so an engine 201 on create can fail the poller `(verify)`. Needs a per-type override or an ADR-0006 note.
- DELETE is async in the spec (200/202/204). A missing disk is 204, not 404.
- PUT is a full replace. `managedBy`, `managedByExtended`, `diskState`, `sku.tier`, `timeCreated` and `uniqueId` are server-owned; ignore them on input.
- `location` is required on a tracked resource. Do not default it to the RG location.
- Disk name: letters, digits, `_` and `-`, up to 80 characters.
- SKU names in the spec: `Standard_LRS`, `Premium_LRS`, `StandardSSD_LRS`, `UltraSSD_LRS`, `Premium_ZRS`, `StandardSSD_ZRS`, `PremiumV2_LRS`.
- `beginGetAccess` and `endGetAccess` are LROs with a `Location` header in the spec. A sync 200 or 204 relies on SDK pollers treating a terminal initial response as done.
- A fake SAS must be ephemeral and never asserted verbatim.

## Dropped

| ID | Reason |
|---|---|
| none | All ten source ids for this service are in Scenarios. |
