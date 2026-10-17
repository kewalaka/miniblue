# App Service

> Engine fit: passthrough or hooks for serverfarms; bespoke for sites, config and Kudu (per type below). miniblue today: hand-written service `internal/services/sites` (chi, no engine). Suggested wave: sites stay bespoke unless gate G5 applies ([PLAN](../PLAN.md), [ADR-0007](../adr/0007-migration-strategy.md)); serverfarms and sub-scope list are SPIKE rank 14 ([SPIKE](../SPIKE.md)).

Spec checked: `Microsoft.Web/AppService/stable/2025-05-01`. The directory listing also shows newer dated folders, so the real latest stable is `(verify)`. In that spec `PUT` on serverfarms, sites and slots returns 200 or 202 (long-running), never 201. See the pitfall on create status.

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.Web/serverfarms` | 2025-05-01 | RG; list by RG and by subscription | none (sites are listed under it, not owned) | `restartSites`; PATCH | passthrough or hooks + gaps | `Hooks{Validate, Defaults}`, `PatchMode`, `Type.Actions`, sub-scope list, per-type create-status override (see pitfall) | H | S |
| `Microsoft.Web/sites` | 2025-05-01 | RG; list by RG and by subscription | config, slots, basicPublishingCredentialsPolicies | `publishxml`; PATCH | hooks + gaps (bespoke unless G5) | `Hooks{Defaults}`, `DefaultIdentity`, sub-scope list, create-status override | H | M |
| `Microsoft.Web/sites/slots` | 2025-05-01 | child of site | config (same set as site) | none | hooks + gaps (bespoke unless G5) | `ChildID`, parent check | M | S |
| `Microsoft.Web/sites/config/{web,appsettings,slotConfigNames,publishingcredentials,...}` | 2025-05-01 | child of site and slot | none | `web` PUT and PATCH; `appsettings` PUT plus POST `appsettings/list`; POST `publishingcredentials/list` | bespoke (singleton sub-resources plus list-POST) | none | M | M |
| `Microsoft.Web/sites/basicPublishingCredentialsPolicies/{ftp,scm}` | 2025-05-01 | child of site | none | none | bespoke (singletons) | none | L | S |
| `Microsoft.Web/checknameavailability` | 2025-05-01 | subscription | none | POST | bespoke (provider-level chi route, [ADR-0001](../adr/0001-single-arm-pipeline.md)) | `CheckName` for the name rule | L | S |
| `Microsoft.Web/webAppStacks` | 2025-05-01 | tenant | none | GET | bespoke (static seed catalog) | built-in seed data | L | S |
| Kudu/SCM: `/api/zipdeploy`, `/api/deployments[/{id}]` | not in the ARM spec `(verify)` | per-site SCM host | deployments | zipdeploy | bespoke | none | L | M |

## miniblue gaps

Ordered by value.

- No list-by-subscription for serverfarms or sites; lists are RG only (`internal/services/sites/handler.go:55`, `:191-192`).
- Site PUT returns 200 on both branches (`handler.go:156-158`) and serverfarms PUT returns 200 on both (`serverfarms.go:127-129`). That matches the spec; keep it if these types ever move.
- Site defaults: `kind` defaults to `functionapp` (`handler.go:109`) and `location` to `eastus`. Real clients send `kind`; a missing `kind` should not imply a function app. The response drops `hostNameSslStates`, `availabilityState` and `siteConfig`.
- Sites are stored under `func:{sub}:{rg}:{name}` keys (`handler.go:92`), plus `func:{sub}:{name}` used for name checks (`handler.go:96`). The prefix is a leftover of the functions code, not a real concept.
- `internal/services/functions/handler.go:35` defines the same `Microsoft.Web/sites` routes as `sites/handler.go:55`, but nothing imports that package, so `sites` is the only live handler. Remove or merge the dead copy before any migration.
- serverfarms:
    - The route is registered as `serverFarms` and responses return `type` `Microsoft.Web/serverFarms` (`serverfarms.go:36`, `:93`). The real, canonical type is `Microsoft.Web/serverfarms`.
    - The SKU tier is derived from a non-spec `properties.skuName` (`serverfarms.go:69-72`). The real SKU is the top-level `sku` object (`name`, `tier`, `size`, `family`, `capacity`).
    - No `restartSites`, no PATCH.
- `config/web` supports GET and PUT only; no PATCH (spec has PATCH). Sites config routes otherwise cover `appsettings`, `azurestorageaccounts`, `authsettings`, `authsettingsV2`, `backup`, `logs`, `metadata`, `connectionstrings`, `pushsettings`, `slotConfigNames`, `publishingcredentials` and the FTP and SCM policies.
- `checknameavailability` handles type `Microsoft.Web/sites` only (`handler.go:219`) and scans one subscription.
- No `publishxml`, no `webAppStacks`, no Kudu endpoints.
- Tests: `tests/functions_test.go` only; there is no dedicated App Service test and no azurerm or azapi fixture for `azurerm_service_plan` or the web app resources.
- `website/docs/services/parity.md:79` lists Azure App Service as not implemented, which is stale.

## Scenarios

### serverfarms

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T33 | Create a Linux B1 plan | `PUT .../serverfarms/{n}` body `{location, kind:"linux", sku:{name:"B1", tier:"Basic"}, properties:{reserved:true}}` => 200; `sku` echoed; `properties.provisioningState` `Succeeded`; `status` `Ready`; `numberOfWorkers` 1; `reserved` from the request | T2 | Spec: 200 or 202, never 201 (see pitfall). `status`, `numberOfWorkers` defaults `(verify)`. |
| G1-T34 | GET, DELETE and list a plan | GET => 200; DELETE => 200 or 204, then GET => 404 `ResourceNotFound`; list by RG and by subscription => 200 containing the plan | T1/T2 | Needs sub-scope list. Deleting a plan that still has sites is rejected with a conflict `(verify)`. |
| G1-T35 | PUT again with a partial body | Full replace, not merge: properties omitted from the second body revert to defaults | T2 | Assert replace semantics. |
| G1-T36 | Restart all sites on a plan | `POST .../serverfarms/{n}/restartSites` => 204, no body | T2 | Spec returns 204 only. Needs `Type.Actions`. |
| G1-T37 | `azurerm_service_plan` Linux B1 and S1, apply and refresh | clean plan; `sku_name` echoed | T3 | New fixture under `tests/terraform/azurerm/`. The azurerm client expects 200 or 202 on PUT `(verify)`. |

### sites

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T38 | Create a site on a plan | `PUT .../sites/{n}` => 200; `defaultHostName` `{n}.azurewebsites.net`; `hostNames` and `enabledHostNames` set; `hostNameSslStates` has main and SCM host entries with `sslState` `Disabled`; `state` `Running`; `availabilityState` `Normal`; `kind` echoes the request; `serverFarmId` echoed; `siteConfig.minTlsVersion` `1.2` | T2 | Property names confirmed against the spec. Exact `hostNameSslStates` entries, default `kind` and default `minTlsVersion` `(verify)`. |
| G1-T39 | GET, DELETE and list a site; GET a missing site | GET => 200; DELETE => 200 or 204; list by RG and by subscription => 200; missing => 404 `ResourceNotFound` | T2 | Spec DELETE also lists 404. Needs sub-scope list. |
| G1-T40 | Name availability for a site | `POST /subscriptions/{s}/providers/Microsoft.Web/checknameavailability` body `{name, type}` => 200; free name `{nameAvailable:true}`; used name `{nameAvailable:false, reason:"AlreadyExists", message}`; invalid name `reason` `Invalid` | T2 | Spec request requires `name` and `type`, optional `isFqdn`; response fields confirmed. Accepted `type` strings `(verify)`. |

### Site configuration

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T41 | `config/web` read and update | `GET .../config/web` => 200; `PUT` and `PATCH` with a subset => 200; only supplied fields change (`linuxFxVersion`, `alwaysOn`, `ftpsState`, `minTlsVersion`, ...) | T2 | Spec has GET, PUT and PATCH, all 200. That PUT merges rather than replaces is `(verify)`. PATCH is missing today. |
| G1-T42 | App settings | `PUT .../config/appsettings` with `properties` map replaces the whole set => 200; `POST .../config/appsettings/list` => 200 with the current map | T2 | Spec has no GET for `appsettings`; keep the existing GET only for compatibility. |
| G1-T43 | Slot config names | `GET .../config/slotConfigNames` => 200 with an empty default structure | T2 | Spec has GET and PUT. Default property names `(verify)`. |
| G1-T44 | Publishing credentials | `POST .../config/publishingcredentials/list` for a site and for a slot => 200 with `publishingUserName`, `publishingPassword`, `scmUri`; the same values on repeat calls | T2 | `publishingUserName` is required in the spec. Real operation is long-running; engine answers sync. `$`-prefixed username format `(verify)`. |

### Static and provider-level

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T45 | Publish profile | `POST .../sites/{n}/publishxml` => 200 with an XML publish profile for the site | T2 | Low value. Request `format` and profile entries `(verify)`. |
| G1-T46 | Web app stacks catalog | `GET /providers/Microsoft.Web/webAppStacks` => 200 static `{value:[...]}` catalog | T2 | Embed a trimmed catalog. Low value. |

### Kudu (bespoke)

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T47 | Kudu deployments | `POST /api/zipdeploy` with basic auth => 202 and a `Location` header; `GET /api/deployments` => list (empty is `[]`); `GET /api/deployments/{id}` => 200, unknown id => 404; missing credentials => 401 with `WWW-Authenticate`; wrong credentials => 401 | T2 | Not an ARM API; low priority. The 202 appears with an async deploy request, a synchronous call blocks and answers 200 `(verify)`. Credentials come from G1-T44. |

## Pitfalls

- Spec create status is 200 or 202 for serverfarms, sites and slots. An engine that returns 201 on create can be rejected by SDK clients that check expected status codes (the go-azure-sdk `webapps` CreateOrUpdate accepts only 200 and 202), so a migrated type needs a per-type create-status override or must stay bespoke. The engine's 201-on-create default is therefore wrong for this provider (`(verify)` against the ADR-0003 hook set).
- A plan PUT is a full replace. Merging omitted properties masks client bugs.
- The SKU is the top-level `sku` object, not a `properties` field. `reserved` distinguishes Linux plans.
- `restartSites` is 204 with no body, not 200.
- `publishingcredentials/list` and `appsettings/list` are POSTs; real Azure has no GET on `config/appsettings`.
- Site names are global (`{name}.azurewebsites.net`); a per-subscription scan cannot report names taken in other subscriptions.
- Do not key sites under another service's prefix. Kind decides the experience (`app`, `app,linux`, `functionapp`); echo what the client sent.
- Real DELETE of a missing site or slot can be 404 per the spec, whereas the engine rule for DELETE is 200 or 204.

## Dropped

| ID | Reason |
|---|---|
| (none) | All source ids G1-T33 to G1-T47 are in the tables above. |
