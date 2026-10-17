# Log Analytics

> Engine fit: hooks for `workspaces`; passthrough for `deletedWorkspaces`; bespoke for the ingestion and query data planes; `sharedKeys` is an engine-gap until `Type.Actions` lands. miniblue today: none (no `internal/services/operationalinsights`; `Microsoft.OperationalInsights` is not in `supportedProviders`, `internal/services/subscriptions/handler.go:94-112`). Suggested wave: new type after G2, in the same PR as [Insights](insights.md).

Workspace-based Application Insights components point at a workspace by resource id, so common azurerm modules need both. Ship `workspaces` first; the Insights page depends on it (its workspace-link and azurerm scenarios).

Scope: the ARM `workspaces` type, `deletedWorkspaces`, and a thin bespoke data plane. Tables, saved searches, linked services, data sources, solutions and clusters are out of scope and have no scenario ids.

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.OperationalInsights/workspaces` | 2026-03-01, 2025-07-01, 2025-02-01 (spec dirs; which azurerm pins is `(verify)`) | RG; list by subscription | tables, linkedServices, dataSources, savedSearches (not scoped) | `POST sharedKeys` and `getSharedKeys` `(verify)` | hooks | `Defaults` with `Request.Current` (stable `customerId`), `PatchMode` merge (`retentionInDays`, tags), `ListSubscription` (PLAN 1.2), RG existence, `Provisioned`, `Type.Actions` for `sharedKeys` | H | S |
| `Microsoft.OperationalInsights/deletedWorkspaces` | same | subscription; RG `(verify)` | none | none | passthrough | none | L | S |
| Ingestion `POST /api/logs` (data plane) | n/a | workspace host | none | `Log-Type` header selects the custom table | bespoke | none (not ARM, stays chi) | L | S |
| Query `POST /v1/workspaces/{id}/query` (data plane) | v1 | workspace id or resource id | none | KQL subset | bespoke | none (not ARM, stays chi) | M | L |

## miniblue gaps

- No service at all: no `workspaces`, no data plane, no provider registration entry (`internal/services/subscriptions/handler.go:94-112`).
- `customerId` (the workspace GUID) must be generated once and stay stable across PUT; `Request.Current` makes that a `Defaults` hook job.
- `sharedKeys` has no home yet. azurerm reads the shared keys after create to fill `primary_shared_key`; whether it tolerates a missing endpoint is `(verify)`. The engine resource-action seam (`Type.Actions`) lands with the first `listKeys` consumer (redis, wave 2). Until then a chi route is the fallback.
- List by subscription depends on the engine seam in PLAN 1.2.
- Ingestion is trivial (accept and acknowledge). Query is the expensive part and should stay a small KQL subset.

## Scenarios

### workspaces

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T77 | Create a workspace with defaults. | `PUT workspaces/{n}` => 201; `properties.customerId` is a GUID; `provisioningState` `Succeeded`; `sku.name` `PerGB2018`; `retentionInDays` 30. | T2 | Real PUT is long-running; sync-only returns the final state. The default retention of 30 for `PerGB2018` is `(verify)`. |
| G2-T78 | `customerId` survives updates. | Repeat `PUT` with changed tags => 200; `customerId` identical to the first response. | T2 | `Defaults` hook copies from `Request.Current`. |
| G2-T79 | Lifecycle and listing. | `GET` => 200; `DELETE` => 200 then `GET` => 404 `ResourceNotFound`; list by RG and by subscription include all created workspaces. | T1 | Real delete is a soft delete (recoverable for a period) `(verify)`; an emulator may hard delete. |
| G2-T80 | Update retention and tags with PATCH. | `PATCH` body `{properties:{retentionInDays}}` => 200 and next `GET` shows it; `PATCH` body `{tags}` => 200 and tags updated; untouched properties unchanged. | T2 | `PatchMerge`. Retention bounds are `(verify)`. |
| G2-T81 | azurerm create and destroy `azurerm_log_analytics_workspace`. | Apply and destroy succeed; `workspace_id` equals `customerId`; `sku` and `retention_in_days` as set. | T3 | New fixture. Whether azurerm tolerates a missing `sharedKeys` endpoint is `(verify)`; safer to serve G2-N11 first. |
| G2-N11 | `new`: shared keys for a workspace. | `POST workspaces/{n}/sharedKeys` => 200 with `primarySharedKey` and `secondarySharedKey`; values identical across calls. | T2 | `new`. Needs `Type.Actions`. Exact path and verb (`sharedKeys` versus `getSharedKeys`) `(verify)`. |

### deletedWorkspaces

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T84 | List soft-deleted workspaces in a subscription. | `GET /subscriptions/{s}/providers/Microsoft.OperationalInsights/deletedWorkspaces` => 200 `{value: []}` on a clean subscription. | T2 | If soft delete is modelled, a deleted workspace appears here `(verify)`. |

### Data plane (bespoke)

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T82 | Post custom log records (one record, many records, several `Log-Type` values). | `POST /api/logs` with a `Log-Type` header => 200, empty body. | T2 bespoke | The real call needs a SharedKey signature and `x-ms-date`; an emulator can skip validation `(verify)`. Missing `Log-Type` is a 400 `(verify)`. |
| G2-T83 | Query a workspace. | `POST /v1/workspaces/{id}/query` => 200; `tables[0].name == "PrimaryResult"`; custom-table rows come back through `take`, `where`, `project`, `summarize count`; `workspace()` resolves by `customerId` or resource id; `union` works. Standard tables such as `AzureActivity` and `AzureDiagnostics` return an empty table. | T2 bespoke | Unknown table is a 400 semantic error in real Azure `(verify)`. Start with `take`, `where`, `count`. |

## Pitfalls

- `customerId` is assigned once. Regenerating it on update breaks every consumer that stored the workspace id.
- Defaults differ by product: workspaces default to 30 days retention, Application Insights components to 90 `(verify)`. Do not share a constant.
- A workspace id in query paths can be the `customerId` GUID or a full resource id; accept both.
- A query for a table that does not exist is an error in real Azure, not an empty result `(verify)`. Standard tables that exist in every workspace return empty.
- A real PUT on an existing workspace returns 200; 201 only on create.
- A missing or malformed api-version is 400; any well-formed version is accepted.
- Real Azure serves `sharedKeys` as a POST. A GET on that path is not the real contract `(verify)`.
- The HTTP Data Collector API (`/api/logs`) is being retired by the real service `(verify)`; keep it minimal.

## Dropped

| ID | Reason |
|---|---|
| none | Nothing dropped; every id in this group is a scenario above. |
