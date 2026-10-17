# Compute

> Engine fit: bespoke (virtualMachines), hooks (availabilitySets), passthrough (SKU and size catalogue). miniblue today: hand-written services `internal/services/vm` (rich) and `internal/services/compute` (catalogue only). Suggested wave: virtualMachines stay bespoke (PLAN wave 5 / G5); `availabilitySets` as a new type after G2.

Paths below are relative to `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Compute` unless stated. Spec facts come from `Microsoft.Compute/Compute` `ComputeRP.json` (read at `2025-04-01`; newer stable directories exist, the newest listed is `2026-04-01`).

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `virtualMachines` | real latest stable `(verify)`; spec read at `2025-04-01`; azurerm pin `(verify)` | RG; list by RG and subscription | extensions | PATCH, power operations, `instanceView`, `runCommand` | bespoke | none; membership link to availability sets is a cross-resource write | M | S (delta only) |
| `availabilitySets` | same (PUT 200, PATCH 200, DELETE 200/204, all sync in the spec) | RG; list by RG and subscription | none | PATCH; `GET {n}/vmSizes`; migration POSTs (not needed) | hooks | `Hooks.Defaults` (fault and update domain counts, `sku`), `PatchTags` or `PatchMerge`, `Under` subscription-wide list, parent-exists (default) | M | S-M |
| `locations/{loc}/vmSizes` | same | subscription, location | none | read-only | passthrough | none; stays the catalogue handler | L | S |
| `Microsoft.Compute/skus` (subscription scope), image publisher, offer, sku and version lists | same | subscription, location | none | read-only | passthrough | none | L | S |

## miniblue gaps

Ordered by value.

- No `availabilitySets` routes or store prefix; no code under `internal/` references `availabilitySet`. VM PUT neither validates `properties.availabilitySet.id` nor records membership, so `azurerm_availability_set` and VMs that join a set cannot work.
- `Microsoft.Compute/skus` entries carry only vCPUs, MemoryGB and MaxDataDiskCount (`compute/handler.go:58`, `:63`); there is no `PremiumIO` capability. The catalogue has two sizes (`compute/handler.go:35`, `:43`).
- DELETE of a missing VM returns 404 (`vm/handler.go:340-342`); real Azure returns 204. DELETE of an existing VM returns 204 (`vm/handler.go:365`), which clients accept.
- VM PUT already checks the resource group and returns 404 `ResourceGroupNotFound` (`vm/handler.go:174`); PATCH merges with `mergePatch` (`vm/arm.go:67`).
- The RG cascade list has `vm:`, `vmsvc:` and `vmext:` only; availability sets need a prefix or `DeleteTree`.

## Scenarios

### Virtual machines

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T112 | PATCH changes `hardwareProfile.vmSize` and tags; list; delete | `PATCH virtualMachines/{v}` with a new `vmSize` and tags => 200, `GET` returns both; list by RG and by subscription include the VM; `DELETE` => 200 or 204, `GET` after => 404 | T2 | Mostly covered by existing tests; check the `vmSize` PATCH. Real PATCH may be 200 or 202 `(verify)` |
| G3-T113 | A VM joins an availability set by id; the set tracks its members | `PUT virtualMachines/{v}` with `properties.availabilitySet.id` of a missing set => 404 (code `(verify)`); with an existing set => 200 or 201 and `GET availabilitySets/{n}` lists the VM under `properties.virtualMachines`; `DELETE` the VM => it is gone from the list | T2 | Depends on the availability set lifecycle row. Cross-resource write, so the VM handler stays bespoke. `virtualMachines` is a list of sub-resource ids in the spec |
| G3-T120 | A multi-resource create in dependency order succeeds | RG, then vnet and subnet, public IP, NSG, NIC, then VM, each => 2xx; `GET` VM => `provisioningState` `Succeeded` | T2/T4 | The `az vm create` command itself needs the az CLI; use its call order as the httptest sequence |

### Availability sets

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T115 | Availability set lifecycle | `PUT availabilitySets/{n}` => 200 (create and update); body has `platformFaultDomainCount`, `platformUpdateDomainCount`, `sku.name`; `GET`; `PATCH` tags => 200; `DELETE` => 200; list by RG and by subscription; missing RG => 404 `ResourceGroupNotFound` | T1/T2 | Spec lists only 200 for PUT, while the engine answers 201 on create (ADR 0006); whether azurerm's `azurerm_availability_set` accepts 201 is `(verify)`. `sku.name` is `Aligned` for managed disks `(verify)`. Missing in miniblue |
| G3-T116 | Delete of a missing availability set | `DELETE availabilitySets/{n}` missing => 204; existing => 200 | T1/T2 | Spec: DELETE 200/204 |
| G3-T117 | `GET availabilitySets/{n}/vmSizes` lists sizes available for new VMs in the set | `GET` => 200 `{"value":[...]}` with `name`, `numberOfCores`, `memoryInMB`, `maxDataDiskCount`; an empty set returns every catalogue size; a set with member VMs returns only the sizes of the members' size family | T2 | Family filtering is an approximation of the real hardware-cluster rule `(verify)` |
| G3-T118 | Availability sets deploy from an ARM template; the set lists its members | deployment with an availability set and VMs referencing it => set `GET` lists the VMs; `azurerm_availability_set` applies and destroys | T2/T3 | The template case depends on deployment dispatch (see the deployments page). Add the Terraform case to `azurerm/compute` |

### Catalogue

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T119 | The SKU list is non-empty and exposes a premium storage capability | `GET /subscriptions/{sub}/providers/Microsoft.Compute/skus` => 200, non-empty; at least one size has capability `PremiumIO` set to `True` | T2 | Existing catalogue test covers non-empty; add the `PremiumIO` capability |

## Pitfalls

- Missing resources are the generic `ResourceNotFound`; do not mint per-type codes (a VM-specific or availability-set-specific not-found code is not a real ARM code `(verify)`).
- DELETE of a missing availability set or VM is 204, not 404.
- Availability set PUT is 200 in the spec even on create; see the note on the lifecycle row.
- `properties.virtualMachines` on a set is maintained by the platform from VM `availabilitySet.id`; clients never write it, so a PUT of the set must not overwrite the member list.
- A VM cannot reference an availability set that does not exist; the id comparison is case-insensitive `(verify)`.
- `vmSizes` for a set is a subset of the size catalogue, not a copy of it.

## Dropped

| ID | Reason |
|---|---|
| G3-T114 | No scenario to add: miniblue already serves VM extensions, power operations, `instanceView` and `vmSizes` |
