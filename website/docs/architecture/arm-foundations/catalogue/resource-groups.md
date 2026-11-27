# Resource groups

> Engine fit is bespoke today for cascade across legacy services, and hooks once served by the engine, with `Validate` for name and location rules and a DELETE-missing override. miniblue today has a hand-written service `internal/services/resourcegroups`. Suggested wave is PLAN 3.3 (`refactor(resourcegroups): serve resource groups via arm engine`), preceded by PLAN 0.6 (cascade via scope cleaners) and PLAN 1.2 (`Engine.DeleteScope`).

Resource groups are the parent scope for every resource-group-scoped type, so their behaviour is shared by all other catalogue pages. The delete-cascade scenario is the acceptance test for every type that migrates (ADR-0004 scope cleaners, ADR-0007 recipe step 6).

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.Resources/resourceGroups` | any well-formed version; spec repo has 2024-11-01, 2025-03-01, 2025-04-01 (latest `(verify)`) | subscription | every RG-scoped type | `HEAD` existence (204/404); `PATCH`; list; `POST exportTemplate` (200/202); `POST moveResources` and `validateMoveResources` (202/204, out of scope) | bespoke now; hooks after PLAN 3.3 | `Hooks.Validate` (name pattern, location immutability), DELETE-missing 404 override (ADR-0006 default is 204), `ScopeExists` native, `Engine.DeleteScope` plus cleaners, `store.SchemaVersion` bump | H | M |
| `GET /subscriptions/{s}/resourcegroups/{rg}/resources` (and `GET /subscriptions/{s}/resources`) | same | subscription, RG | none | `$filter`, `$expand` (`createdTime`, `changedTime`, `provisioningState`), `$top` | engine-gap | prefix-scan list across engine types plus subscription-wide list (PLAN 1.2); legacy types stay invisible until they migrate | M | S |

Real resource group facts (confirmed in the REST spec, api-version 2025-04-01):

- Name is 1 to 90 characters matching `^[-\w\._\(\)]+$`, case-insensitive.
- Body is a tracked resource (`location` required on PUT) plus `managedBy` and `properties.provisioningState`.
- PUT returns 200 or 201, PATCH returns 200, HEAD returns 204 or 404, and DELETE returns 200 or 202 (the spec lists no 204).
- DELETE takes an optional `forceDeletionTypes` query parameter. Deleting a group deletes its deployments and their stored operations.

## miniblue gaps

Ordered by value.

- Cascade is incomplete and hard-coded. `handler.go:141-205` is a literal `DeleteByPrefix` list that imports aks and vm for cleanup. The list omits the `deployment:` prefix (deployments survive resource group deletion, whereas real Azure removes them), keyvault, and every `arm:` engine key (index finding 6). The engine keys are covered by PLAN 1.2 `DeleteScope`, and the keyvault cleaner by PLAN 0.6.
- `/resources` always returns an empty list (`handler.go:215`). Real Azure lists every resource in the group with `id`, `name`, `type`, `location`, and `tags`. This breaks `az resource list`, `terraform` data sources and any check for whether a resource group is empty. PLAN 3.3 serves it from the engine.
- DELETE is async-shaped. Deleting an existing resource group returns 202 with a `Location` to `operationresults/delete-rg-{name}` (`handler.go:197-204`), and the operation result returns a bare 200 (`handler.go:221`). PLAN 3.3 makes DELETE synchronous 200 and drops `operationresults`. Tests must accept 200 and 202 until then.
- PATCH panics after a persisted restart. `handler.go:105` type-asserts the stored value to `ResourceGroup`; a value reloaded from the persistence backend is a map, not the struct (index finding 1). The engine's uniform JSON storage fixes this (ADR-0004).
- PUT has no validation. It skips name pattern and length checks, location immutability checks, and subscription checks. `location` is required (400 `InvalidRequestContent`).
- GET and DELETE on a missing resource group return `ResourceNotFound`, not `ResourceGroupNotFound` `(verify)`. `tests/resourcegroups_test.go` asserts the current code; update it with the migration.
- `tags` is omitted when empty (`omitempty` at `handler.go:20`), and there is no `managedBy`.
- PATCH only handles tags and ignores `managedBy` and `properties`.
- Existing coverage includes `tests/resourcegroups_test.go` (create, duplicate, PATCH tags, not found, HEAD, vnet-only delete cascade, missing location) and `tests/cascade_test.go` (all legacy services plus a cross-RG VM case).

## Scenarios

### Resource group CRUD

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T1 | PUT a new resource group then GET it. Body has `id`, `name`, `type=Microsoft.Resources/resourceGroups`, `location`, and `properties.provisioningState=Succeeded`. A second identical PUT is an update. | PUT returns 201 then a second PUT returns 200; GET returns 200. | T1/T2 | Real Azure omits `tags` on a tagless resource group `(verify)`; accept absent or `{}` and reject `null`. Add `managedBy` round-trip. Covered in part by `tests/resourcegroups_test.go`. |
| G3-T2 | PATCH sets tags (`env=prod`, `team=platform`). Tags persist on GET and `location` is unchanged. PATCH on a missing resource group fails. | PATCH returns 200 with tags set and location unchanged; PATCH on a missing group returns 404 `ResourceGroupNotFound`. | T2 | Whether PATCH replaces or merges the tag map is `(verify)`; pick one and assert it (`PatchMode`). Today's missing-RG code is `ResourceNotFound`. |
| G3-T3 | PUT an existing resource group with no `tags` replaces the tag map with an empty one. | PUT returns 200 with tags empty or absent. | T2 | PUT is replace, not merge. Counterpart to T2. |
| G3-T4 | List returns exactly the N groups created (`rg1` to `rg3`) and none belonging to another subscription. | GET `/subscriptions/{s}/resourcegroups` returns 200, and `value` has N items. | T2 | Assert as a set, because real Azure gives no ordering guarantee. The cross-subscription half needs a second subscription and is conditional on a subscription store (see Subscriptions and providers). |
| G3-T5 | DELETE a missing resource group fails; DELETE an existing resource group succeeds and the group is then gone. | Missing returns 404 `ResourceGroupNotFound` `(verify)`; existing returns 202 with `Location` today and 200 after PLAN 3.3; a subsequent GET returns 404. | T2 | Spec allows 200 or 202 and no 204. Accept both until PLAN 3.3. azurerm tolerance of a synchronous 200 is `(verify)`. Covered, spot check only. |
| G3-T7 | PUT a resource group in a subscription that does not exist fails. | Returns 404 `SubscriptionNotFound` `(verify)`. | T2 | Conditional. Only meaningful once a subscription store exists. With one fixed subscription the engine never sees an unknown id. |
| G3-T8 | PUT that changes `location` of an existing resource group fails. PUT with an invalid location fails. | Returns 4xx `InvalidResourceGroupLocation` (verify status; message says the group already exists in another location); an invalid location returns 400 `(verify)`. | T1/T2 | `Hooks.Validate` using `Request.Current`. Location compare is case-insensitive and ignores spaces `(verify)`. |

### Delete cascade

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T6 | Cascade matrix. Create a storage account with a blob container, a SQL server with a database and firewall rule, a Service Bus namespace with queue, topic, subscription and rule, a VNet with subnet and peering, an availability set, and a deployment. DELETE the resource group. Every child GET afterwards returns not found and no list shows them. | Each child GET returns 404 (`ResourceNotFound` or `ResourceGroupNotFound`); every list returns `value` without the deleted items. | T2 | Acceptance test for every migrated type (ADR-0007 step 6). Include a deployment, which is missing from the cascade today. Each new type needs a cleaner or engine `DeleteScope` coverage, with no orphan prefix. Child types that do not exist yet (Service Bus subscriptions and rules, SQL firewall rules, VNet peerings) are skipped, not stubbed; add each row when its type lands. |

## Pitfalls

- Resource group DELETE never returns 204 in real Azure. It returns 200 or 202 when the group existed and 404 when it did not. Asserting 204 for a missing group is wrong, and the engine's default of 204 on a missing resource (ADR-0006) needs an override for this type.
- PUT in an unknown subscription returns 404, not a server error.
- Location is immutable once the group exists. A PUT that changes it fails; it is not a silent update.
- A list of resource groups has no defined order. Assert sets, never positions.
- `GET .../resources` must include resources in the group regardless of which service owns them. An empty list is a visible bug, not a safe default.
- After a restart with `PERSISTENCE=1`, stored values come back as generic JSON. Any handler that type-asserts a stored struct breaks (the PATCH panic above).
- Long-running-operation pollers wait for the group to be gone. A 202 whose `Location` never completes hangs them; a synchronous 200 is the safe shape for a single-process emulator.

## Dropped

None. Resource group `exportTemplate` scenarios are on [Deployments and ResourceManager](deployments.md).
