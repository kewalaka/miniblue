# Insights (Application Insights)

> Engine fit is hooks for `components`, and bespoke for `currentbillingfeatures` and for the ingestion and query data planes. miniblue today has no `internal/services/insights` package, and `Microsoft.Insights` is not in `supportedProviders` (`internal/services/subscriptions/handler.go:94-112`). Suggested wave is a new type after G2, shipped in the same PR as [Log Analytics](log-analytics.md).

Common azurerm modules create a Log Analytics workspace and a workspace-based `azurerm_application_insights` that points at it (`workspace_id`). Treat the two pages as one deliverable. Components without workspaces fail the first `terraform apply` of most modules.

Scope covers the ARM control plane (`components` and `currentbillingfeatures`) plus a small bespoke data plane for telemetry ingestion and query. Alerts, workbooks, web tests, `eventtypes` and API keys are out of scope and have no scenario ids.

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.Insights/components` | 2020-02-02, 2015-05-01 (spec dirs; latest stable `(verify)`) | RG; list by subscription | `currentbillingfeatures` | none | hooks | `Defaults` with `Request.Current` (stable `AppId`, `InstrumentationKey`, `ConnectionString`), `ListSubscription` (PLAN 1.2), `PatchMode` tags (real PATCH is a tags update `(verify)`), RG existence, `Provisioned` | H | S |
| `components/currentbillingfeatures` | 2015-05-01 | singleton under a component (no name segment `(verify)`) | none | none | bespoke | parent existence (`ParentResourceNotFound`) | L | S |
| Ingestion `POST /v2/track`, `/v2.1/track` (data plane) | not applicable | host from the component's `IngestionEndpoint` | none | batch envelope | bespoke | none (not ARM, stays chi) | M | M |
| Query `POST /v1/apps/{appId or ikey}/query` (data plane) | v1 | host from the component's endpoint | none | KQL subset | bespoke | none (not ARM, stays chi) | M | L |

Property names on `components` are PascalCase in this provider (`Application_Type`, `Flow_Type`, `Request_Source`, `WorkspaceResourceId`, `InstrumentationKey`, `ConnectionString`, `AppId`, `RetentionInDays`, `IngestionMode`) `(verify)`. Engine responses are canonical-cased (ADR-0005), so pin the casing in `Sample` and check azapi round trips.

## miniblue gaps

- No Insights service exists yet, including `components`, `currentbillingfeatures`, and the data plane. Everything below is greenfield.
- `Microsoft.Insights` and `Microsoft.OperationalInsights` are missing from `supportedProviders` (`internal/services/subscriptions/handler.go:94-112`), so provider list and register calls do not show them. Whether azurerm's registration check reads this list is `(verify)`.
- Stable generated values (`AppId`, `InstrumentationKey`) need either derivation from the resource id or `Request.Current` copy-forward in a `Defaults` hook. Regenerating per PUT breaks cached connection strings.
- `ConnectionString` endpoint hosts must point at miniblue, otherwise SDKs ship telemetry out of the emulator.
- List by subscription depends on the engine seam in PLAN 1.2.
- Ingestion and KQL query are not started. Query is the expensive part. Scope it to `take`, `where`, and `count` first.

## Scenarios

### components

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T63 | Create, read, delete a component. | `PUT components/{n}` returns 201 with `properties`; `GET` returns 200; `DELETE` returns 200; `GET` after returns 404 `ResourceNotFound`. A second `DELETE` returns 204 `(verify)`. | T1 | Generic lifecycle. The real DELETE is documented as 200 or 204 `(verify)`. |
| G2-T64 | A new component exposes a GUID instrumentation key and a connection string built from it. | `GET` returns 200; `InstrumentationKey` is a GUID; `ConnectionString` starts with `InstrumentationKey={key};` and carries `IngestionEndpoint=` and `LiveEndpoint=`; `AppId` is a GUID. | T2 | Endpoint hosts must be miniblue's. Keys stay identical across later PUT and PATCH. |
| G2-T65 | List by resource group returns every component created in it. | `GET .../resourceGroups/{rg}/providers/Microsoft.Insights/components` returns 200 with all created names present and none from other groups. | T1 | |
| G2-T66 | List by subscription returns all components. | `GET /subscriptions/{s}/providers/Microsoft.Insights/components` returns 200 with all created names present. | T1 | Needs `ListSubscription` (PLAN 1.2). |
| G2-T67 | Update tags with PATCH. | `PATCH components/{n}` with body `{tags}` returns 200; the next `GET` shows the new tags and leaves other properties unchanged. | T1 | `PatchTags` `(verify)`. |
| G2-T68 | Deploy a component with a workspace link through an ARM deployment. | Deployment succeeds; `GET components/{n}` returns `WorkspaceResourceId` equal to the template value with case preserved. | T2 | Goes through the deployments service. Depends on [Log Analytics](log-analytics.md) workspaces existing. |
| G2-T73 | azurerm creates and destroys a workspace-based `azurerm_application_insights`. | Apply and destroy succeed; `application_type` matches what was set; `instrumentation_key` is populated; `connection_string` contains the key; `daily_data_cap_in_gb` is persisted. | T3 | Needs T69 and T71. New fixture because no Insights fixture exists today. Pair with the workspace fixture. |

### currentbillingfeatures

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T69 | Default billing features for a new component. | `GET components/{n}/currentbillingfeatures` returns 200; the plan list contains `Basic` and `DataVolumeCap` is present. | T2 | Response is a flat object, not under `properties` `(verify)`. |
| G2-T70 | Switch the plan list to include the Enterprise plan. | `PUT` with `CurrentBillingFeatures` including `Application Insights Enterprise` returns 200; the next `GET` lists it. | T2 | The retention limit that triggers this is unclear (30 versus 90 days in the notes) `(verify)`. Treat the retention link as unconfirmed and assert only the persisted plan list. |
| G2-T71 | Set a daily data cap. | `PUT` with body `DataVolumeCap.Cap` returns 200; the next `GET` returns the same cap. | T2 | azurerm reads this for `daily_data_cap_in_gb`. |
| G2-T72 | Billing features for a missing component. | `GET` or `PUT` returns 404 `ResourceNotFound`. | T2 | The engine rule for a missing parent is `ParentResourceNotFound`; which code this provider returns is `(verify)`. |

### Data plane (bespoke)

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T74 | Post telemetry items (request, trace, exception, event, metric, dependency, and a batch). | `POST /v2/track` and `/v2.1/track` return 200 with `{itemsReceived, itemsAccepted, errors}`; an unknown instrumentation key returns 401 `(verify)`. | T2 bespoke | Real ingestion answers 206 on partial success `(verify)`. The 401 for an unknown key is unconfirmed. |
| G2-T75 | Query ingested telemetry. | `POST /v1/apps/{key}/query` returns 200 with `tables[0].name == "PrimaryResult"`; `take`, `where`, and `count` return the ingested rows. | T2 bespoke | Start with `take`, `where`, and `summarize count`. Defer `bin`, `join`, `ago`, and `between`. An unknown table is a 400 query error in real Azure `(verify)`, not an empty result. |

## Pitfalls

- `AppId`, `InstrumentationKey` and `ConnectionString` are stable for the life of the component. Real Azure never changes them on update.
- The retention default differs by product. Components default to 90 days and Log Analytics workspaces default to 30 `(verify)`. Copying one default to the other is wrong.
- A missing or malformed api-version returns 400; any well-formed version is accepted. Do not return 400 for an api-version the spec does not list.
- Real ARM PUT on an existing resource returns 200, not 201. Asserting 201 on update is wrong.
- A query on a table that does not exist returns an error in real Azure `(verify)`. An emulator may return an empty table, but the doc assertion is the error.
- `currentbillingfeatures` has no resource name segment in its path `(verify)`. Engine-registered child types assume `{parent}/{type}/{name}`.
- Telemetry ingestion with a partial failure returns 206, and the envelope's `errors` carry per-item `index` and `statusCode` `(verify)`.

## Dropped

| ID | Reason |
|---|---|
| G2-T76 | PowerShell wrappers over the same operations as T63 to T67 are not portable and add no new behaviour. |
