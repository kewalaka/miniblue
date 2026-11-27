# Log Analytics

> Engine fit is hooks for `workspaces`, passthrough for `deletedWorkspaces`, and bespoke for the ingestion and query data planes. `sharedKeys` is an engine gap until `Type.Actions` lands. miniblue today has no `internal/services/operationalinsights` package, and `Microsoft.OperationalInsights` is not in `supportedProviders` (`internal/services/subscriptions/handler.go:94-112`). Suggested wave is a new type after G2, in the same PR as [Insights](insights.md).

Workspace-based Application Insights components point at a workspace by resource id, so common azurerm modules need both. Ship `workspaces` first, because the Insights page depends on it for its workspace-link and azurerm scenarios.

Scope covers the ARM `workspaces` type, `deletedWorkspaces`, and a thin bespoke data plane. Tables, saved searches, linked services, data sources, solutions and clusters are out of scope and have no scenario ids.

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.OperationalInsights/workspaces` | 2026-03-01, 2025-07-01, 2025-02-01 (spec dirs; which azurerm pins is `(verify)`) | RG; list by subscription | tables, linkedServices, dataSources, savedSearches (not scoped) | `POST sharedKeys` and `getSharedKeys` `(verify)` | hooks | `Defaults` with `Request.Current` (stable `customerId`), `PatchMode` merge (`retentionInDays`, tags), `ListSubscription` (PLAN 1.2), RG existence, `Provisioned`, `Type.Actions` for `sharedKeys` | H | S |
| `Microsoft.OperationalInsights/deletedWorkspaces` | same | subscription; RG `(verify)` | none | none | passthrough | none | L | S |
| Ingestion `POST /api/logs` (data plane) | not applicable | workspace host | none | `Log-Type` header selects the custom table | bespoke | none (not ARM, stays chi) | L | S |
| Query `POST /v1/workspaces/{id}/query` (data plane) | v1 | workspace id or resource id | none | KQL subset | bespoke | none (not ARM, stays chi) | M | L |

## miniblue gaps

- No Log Analytics service exists yet, including `workspaces`, the data plane, and the provider registration entry (`internal/services/subscriptions/handler.go:94-112`).
- `customerId` (the workspace GUID) must be generated once and stay stable across PUT. `Request.Current` makes that a `Defaults` hook job.
- `sharedKeys` has no home yet. azurerm reads the shared keys after create to fill `primary_shared_key`, and whether it tolerates a missing endpoint is `(verify)`. The engine resource-action seam (`Type.Actions`) lands with the first `listKeys` consumer (redis, wave 2). Until then a chi route is the fallback.
- List by subscription depends on the engine seam in PLAN 1.2.
- Ingestion is simple (accept and acknowledge). Query is the expensive part and should stay a small KQL subset.

## Scenarios

### workspaces

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T77 | Create a workspace with defaults. | `PUT workspaces/{n}` returns 201; `properties.customerId` is a GUID; `provisioningState` is `Succeeded`; `sku.name` is `PerGB2018`; `retentionInDays` is 30. | T2 | Real PUT is long-running; sync-only returns the final state. The default retention of 30 for `PerGB2018` is `(verify)`. |
| G2-T78 | `customerId` survives updates. | A repeat `PUT` with changed tags returns 200 and keeps `customerId` identical to the first response. | T2 | `Defaults` hook copies from `Request.Current`. |
| G2-T79 | Lifecycle and listing. | `GET` returns 200; `DELETE` returns 200 and `GET` after returns 404 `ResourceNotFound`; list by RG and by subscription include all created workspaces. | T1 | Real delete is a soft delete that is recoverable for a period `(verify)`. An emulator may hard delete. |
| G2-T80 | Update retention and tags with PATCH. | `PATCH` with body `{properties:{retentionInDays}}` returns 200 and the next `GET` shows it; `PATCH` with body `{tags}` returns 200 and updates tags while leaving untouched properties unchanged. | T2 | `PatchMerge`. Retention bounds are `(verify)`. |
| G2-T81 | azurerm creates and destroys `azurerm_log_analytics_workspace`. | Apply and destroy succeed; `workspace_id` equals `customerId`; `sku` and `retention_in_days` match what was set. | T3 | New fixture. Whether azurerm tolerates a missing `sharedKeys` endpoint is `(verify)`. It is safer to serve G2-N11 first. |
| G2-N11 | `new`. Shared keys for a workspace. | `POST workspaces/{n}/sharedKeys` returns 200 with `primarySharedKey` and `secondarySharedKey`, identical across calls. | T2 | `new`. Needs `Type.Actions`. Exact path and verb (`sharedKeys` versus `getSharedKeys`) are `(verify)`. |

### deletedWorkspaces

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T84 | List soft-deleted workspaces in a subscription. | `GET /subscriptions/{s}/providers/Microsoft.OperationalInsights/deletedWorkspaces` returns 200 with `{value: []}` on a clean subscription. | T2 | If soft delete is modelled, a deleted workspace appears here `(verify)`. |

### Data plane (bespoke)

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T82 | Post custom log records (one record, many records, several `Log-Type` values). | `POST /api/logs` with a `Log-Type` header returns 200 with an empty body. | T2 bespoke | The real call needs a SharedKey signature and `x-ms-date`, and an emulator may skip validation `(verify)`. Missing `Log-Type` returns 400 `(verify)`. |
| G2-T83 | Query a workspace. | `POST /v1/workspaces/{id}/query` returns 200 with `tables[0].name == "PrimaryResult"`; custom-table rows come back through `take`, `where`, `project`, and `summarize count`; `workspace()` resolves by `customerId` or resource id; `union` works. Standard tables such as `AzureActivity` and `AzureDiagnostics` return an empty table. | T2 bespoke | An unknown table is a 400 semantic error in real Azure `(verify)`. Start with `take`, `where`, and `count`. |

## Pitfalls

- `customerId` is assigned once. Regenerating it on update breaks every consumer that stored the workspace id.
- Defaults differ by product. Workspaces default to 30 days retention and Application Insights components default to 90 `(verify)`. Do not share a constant.
- A workspace id in query paths can be the `customerId` GUID or a full resource id; accept both.
- A query for a table that does not exist returns an error in real Azure, not an empty result `(verify)`. Standard tables that exist in every workspace return empty.
- A real PUT on an existing workspace returns 200, and returns 201 only on create.
- A missing or malformed api-version returns 400; any well-formed version is accepted.
- Real Azure serves `sharedKeys` as a POST. A GET on that path is not the real contract `(verify)`.
- The HTTP Data Collector API (`/api/logs`) is being retired by the real service `(verify)`, so keep it minimal.

## Dropped

| ID | Reason |
|---|---|
| none | Nothing is dropped; every id in this group is a scenario above. |
