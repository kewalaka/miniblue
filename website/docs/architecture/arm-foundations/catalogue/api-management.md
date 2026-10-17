# API Management

> Engine fit: hooks for every Tier A type (`service`, `apis`, `operations`, `products`, `namedValues`, `backends`), passthrough for `products/apis`, engine-gap for the Tier B singletons and `listSecrets`. miniblue today: none (no `internal/services/apim`, no registration, no test, no Terraform fixture). Suggested wave: [Slice 2](../slices/02-apim-consumption.md) (PLAN 2.4 and 2.5) for Tier A; Tier B and the follow-on types after G2.

The MVP slice (types, hooks, defaults, acceptance, PRs) is in [Slice 2](../slices/02-apim-consumption.md) and is not repeated here. This page holds the full resource table, every `APIM-T` scenario with its tier, the cost-tier test plan and the pitfalls. Provider call sequences and the defaults azurerm dereferences are in [API Management: provider notes](api-management-provider-notes.md).

**Gateway runtime is out of scope.** No proxying through APIs, no policy execution, no subscription-key validation, no `*.azure-api.net` host handling. The derived `gatewayUrl` is a string that does not resolve. Policies are stored as opaque XML. Nothing on this page covers a data plane.

Tiers used below: **Tier A** is the Consumption SKU path (`Consumption_0`), **Tier B** adds what azurerm needs for `Developer_1` and above (portal settings, tenant access, `listSecrets`).

## Types

Latest stable api-version is 2024-05-01 (confirmed in the REST spec repo; older stable versions run from 2016-07-07 to 2022-08-01, previews to 2025-09-01-preview). azurerm pins the service and backends at 2024-05-01 and most other children at 2022-08-01; pins move between provider releases (verify per release). Types therefore list both versions. `APIVersions` is metadata only (ADR-0005), so extra versions never produce `InvalidApiVersionParameter`.

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.ApiManagement/service` | 2024-05-01, 2022-08-01, 2021-08-01 | RG | apis, products, namedValues, backends (Tier A); all rows below | provider-level `POST checkNameAvailability`; `GET/DELETE locations/{loc}/deletedservices/{n}`; `POST` backup, restore, migrateToStv2, applynetworkconfigurationupdates, getssotoken | hooks | `Request.Current` (stable URLs and identity), `PatchMode` merge (azurerm updates with PATCH), `ListSubscription`, RG existence, `Provisioned` | H | M |
| `service/apis` (name may be `id;rev=N`) | 2022-08-01, 2024-05-01 | child of service | operations, policies, releases, revisions, diagnostics, schemas, tags | `GET apis/{id}/revisions`; `GET ?export=true`; `DELETE ?deleteRevisions=` | hooks | opaque `;rev=` name, parent existence | H | S to M |
| `service/apis/operations` | 2022-08-01 | child of apis | policies | none | hooks | parent existence | H | S |
| `service/products` | 2022-08-01 | child of service | apis (assignment), groups, policies, subscriptions, tags | `DELETE ?deleteSubscriptions=true` | hooks | none | H | S |
| `service/products/apis` | 2022-08-01 | child of products | none | `HEAD` existence check (204) | passthrough | echoes the API contract | M | S |
| `service/namedValues` | 2022-08-01 | child of service | none | `POST listValue`, `POST refreshSecret` | hooks | `Type.Actions` for both POSTs (later); secret masking has no seam | H | S |
| `service/backends` | 2024-05-01, 2022-08-01 | child of service | none | `POST reconnect` (202 in the spec) | hooks | `Type.Actions` (later); 202 conflicts with sync-only (ADR-0006) | M | S |
| `service/policies/policy` | 2022-08-01 | child of service | none | `GET ?format=` conversion between xml and rawxml | passthrough | format-dependent GET has no seam; azurerm tolerates 404 on read | M | S |
| `service/portalsettings/{signin,signup,delegation}` | 2022-08-01 | child of service | none | `POST delegation/listSecrets` | engine-gap | seeded singleton that GETs 200 before any PUT; `Actions` | M (Tier B only) | S |
| `service/tenant/access` | 2022-08-01 | child of service | none | `POST listSecrets`, `POST regeneratePrimaryKey`, `POST regenerateSecondaryKey` | engine-gap | seeded singleton; `Actions` | M (Tier B only) | S |
| `service/subscriptions` | 2022-08-01 | child of service | none | `POST listSecrets`, regenerate keys | hooks | `Actions`; seeded `master` subscription | H | M |
| `service/groups`, `service/groups/users`, `service/users` | 2022-08-01 | child of service | users | users: `generateSsoUrl`, `token` | hooks | seeded system groups `administrators`, `developers`, `guests` | M | S |
| `service/loggers`, `service/diagnostics`, `service/apis/diagnostics` | 2022-08-01 | child of service or apis | none | none | passthrough | `credentials` masking has no seam | M | S |
| `service/certificates` | 2022-08-01 | child of service | none | none | hooks | `data` and `password` masking has no seam | M | S |
| `service/{products,apis}/policies/policy`, `operations/policies/policy` | 2022-08-01 | scoped policy singleton | none | `GET ?format=` | passthrough | nested `Children` chain | M | S |
| `service/{identityProviders,authorizationServers,openidConnectProviders,authorizationProviders}` | 2022-08-01 | child of service | none | `POST listSecrets` | hooks | `Actions`; secrets masked on GET in real Azure | L to M | S each |
| `service/{tags,apiVersionSets,schemas,policyFragments,policyRestrictions,templates,notifications,contentTypes,portalConfigs,documentations}` | 2022-08-01 | child of service | none | none | passthrough | none | L to M | S each |
| `service/apis/{releases,tagDescriptions,wikis,issues}` | 2022-08-01 | child of apis | none | none | passthrough | none | L | S |
| `service/gateways` (+ `hostnameConfigurations`, `apis`) | 2022-08-01 | child of service | hostnameConfigurations, apis | `listKeys`, `regenerateKey`, `generateToken` | hooks | `Actions`; self-hosted gateway is not worth it | L | M |
| `service/privateEndpointConnections`, `outboundNetworkDependenciesEndpoints`, `networkStatus`, `skus` | 2022-08-01 | child of service | none | list-only | bespoke (static responses) | none | L | S |
| `service/workspaces/*` | 2023+ versions | child of service | many | none | passthrough | none | L | M |

Everything without an azurerm resource (workspaces, portal configs, content types, templates, notifications) is reachable only through azapi and bicep. Those clients can hit any type at any api-version the type lists.

## miniblue gaps

Ordered by value. There are no existing APIM bugs to cite: the repo has no APIM code.

- No `Microsoft.ApiManagement` registration, so every azurerm, azapi and bicep call falls through to the not-found path. Slice 2 closes this for Tier A.
- Every `service` GET must carry the fields azurerm dereferences (`customProperties`, derived URLs, stable identity). That needs `Request.Current` (ADR-0003) so generated values survive repeated PUTs.
- azurerm updates the service with PATCH of `sku`, `properties`, `identity` and `tags`. Needs `PatchMerge` (PLAN 1.3). azapi `azapi_update_resource` sends a full PUT instead.
- No `tests/terraform/azurerm/apim_consumption` or azapi APIM fixture, and no matrix entry in `.github/workflows/acceptance-azurerm.yml`.
- `deletedservices` is a strict 404 with no route (Slice 2). Soft-delete, purge and `recover_soft_deleted` have no seam: `Hooks` has no delete-time hook, so this needs an ADR-0003 amendment or a bespoke route.
- `checkNameAvailability` is not served. It is a provider-level route, so it stays a chi route (ADR-0001) and is a later PR.
- Tier B has no seam: seeded singletons that GET 200 before any PUT, and `listSecrets`. chi ARM routes are frozen after G1 (ADR-0001), so this needs `Type.Actions` plus a seed mechanism (ADR amendment).
- ETag and `HEAD`: the engine sends no ETags and has no HEAD op (ADR-0003). Optional for azurerm, needed only for the ETag scenarios marked deferred below.
- Secret masking, paging (`$top`, `$skip`, `nextLink`) and `$filter` are not available. Documented deviations.

## Scenarios

Ids are stable. Tier is the test layer (T1 armtest row, T2 `tests/arm_apim_test.go` httptest or hook unit test, T3 Terraform, T4 client smoke). "Slice 2" in Notes means the scenario is inside the Consumption MVP; everything else is follow-on or deferred.

### Service

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| APIM-T1 | Create a Developer, capacity 1 service with location, tags, publisher fields in an existing RG | `PUT …/service/{n}` => 201; `type=Microsoft.ApiManagement/service`, `sku{Developer,1}`, `provisioningState=Succeeded`, publisher fields and tags echoed | T1/T2 | Slice 2. armtest PutCreate and ProvisioningState cover the envelope; the sku and publisher echo is T2 |
| APIM-T2 | PUT the same name again | => 200, document replaced; fields absent from the second body are gone | T1 | Slice 2. armtest PutReplace |
| APIM-T3 | GET an existing and an unknown service | => 200; 404 `ResourceNotFound` with the error envelope | T1 | Slice 2. armtest Get and GetMissing |
| APIM-T4 | PATCH `tags`, then PATCH `sku` or `properties.publisherName` | `PATCH` => 200; changed fields updated, untouched `sku` and `properties` kept | T1/T2 | Slice 2 (`PatchMerge`). azurerm update is PATCH. verify: whether real APIM merges or replaces `tags` on PATCH |
| APIM-T5 | DELETE then GET | `DELETE` => 200 when it existed, 204 when missing; then GET => 404 | T1 | Slice 2. armtest Delete and DeleteMissing. The spec allows 200, 202 or 204 (long-running); sync-only per ADR-0006 |
| APIM-T6 | List by RG and by subscription | `GET …/resourceGroups/{rg}/providers/Microsoft.ApiManagement/service` and `GET /subscriptions/{s}/providers/Microsoft.ApiManagement/service` => 200 `{value:[…]}` containing the created services | T1 | Slice 2. armtest List and ListSubscription |
| APIM-T7 | Create with `publisherEmail`, `publisherName`, `sku` or `location` missing, or a publisher field over 100 chars | `PUT` => 400; code `ValidationError` (verify); `target` is the property path | T2 | Slice 2. Spec confirms required: `location`, `sku`, `properties`, `publisherEmail`, `publisherName`; max 100 on the publisher fields. Hook table test |
| APIM-T8 | Create with an invalid name: leading digit, underscore, trailing `-`, 51 chars | `PUT` => 400 `InvalidResourceName` (verify real APIM code) | T2 | Slice 2. Spec name: `^[a-zA-Z](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?$`, 1 to 50. `CheckName` hook |
| APIM-T9 | PUT in a resource group that does not exist | `PUT` => 404 `ResourceGroupNotFound` | T2 | Slice 2. ADR-0003 phase 5, applies to every engine type |
| APIM-T10 | checkNameAvailability for a free name | `POST /subscriptions/{s}/providers/Microsoft.ApiManagement/checkNameAvailability` body `{name}` => 200 `{nameAvailable:true}`; `reason` for an available name is `Valid` (verify) | T2 | Later PR; provider-level chi route (ADR-0001), not in Slice 2 |
| APIM-T11 | checkNameAvailability for the name of an existing service | => 200 `{nameAvailable:false, reason:"AlreadyExists", message}` | T2 | Later PR. Subscription-local scan via `Reader.Under`; real Azure is DNS-global |
| APIM-T12 | checkNameAvailability for an invalid name | => 200 `{nameAvailable:false, reason:"Invalid", message}` | T2 | Later PR. Spec reason enum: `Valid`, `Invalid`, `AlreadyExists` |
| APIM-T13 | `GET locations/{loc}/deletedservices/{n}` before and after deleting the service | before: 404. After delete: 200 with `serviceId`, `location`, `scheduledPurgeDate`, `deletionDate` | T2 | Slice 2 asserts the 404 half only (strict 404, no route). The 200 half needs soft-delete (no seam) |
| APIM-T14 | Purge a soft-deleted service, then recreate it; azurerm `purge_soft_delete_on_destroy=true` flow | `DELETE …/deletedservices/{n}` => 200, 202 or 204 (spec); name is free afterwards | T2/T3 | Unsupported in Slice 2. Needs soft-delete. azurerm requires the preceding GET to succeed, so a bare no-op DELETE is not enough. Nightly fixture when built |
| APIM-T15 | Recreate a name after delete without purge or restore | Real: rejected while the soft-deleted record exists (code verify); `properties.restore=true` recovers it. Slice 2: 201 | T2 | Documented deviation in Slice 2; assert the lenient behaviour only until soft-delete lands |
| APIM-T17 | `az apim create`, `show`, `list`, `update`, `delete`, `check-name` | `provisioningState=Succeeded`, `sku.name=Developer` | T4 | Deferred until an az harness exists (SPIKE §4). The wire calls are the service rows above |
| APIM-T18 | azurerm `Developer_1` with `tenant_access` and `sign_in`: apply, plan, destroy | apply succeeds; `plan -detailed-exitcode` is 0; destroy is clean | T3 | Tier B. Nightly fixture `apim_developer`, not Slice 2 |
| APIM-T19 | azurerm `Consumption_0` service alone: apply, plan, destroy | same three steps as above | T3 | Slice 2 (opt-in fixture `apim_consumption`) |
| APIM-T20 | Response shape of a created service | `customProperties` object present; `virtualNetworkType=None`; `publicNetworkAccess=Enabled`; derived `gatewayUrl`, `gatewayRegionalUrl`, `managementApiUrl`, `scmUrl`, `portalUrl`, `developerPortalUrl`; `provisioningState=Succeeded` | T2 | Slice 2. Spec marks the URLs, IPs and `provisioningState` read-only; exact URL shapes are verify |
| APIM-T21 | `identity.type=SystemAssigned` on repeated PUTs | `principalId` and `tenantId` set and identical across PUTs | T1 | Slice 2. `DefaultIdentity` with `Current`; armtest DefaultsIdempotent |
| APIM-T52 | Headers on write responses | no `Azure-AsyncOperation`, no `Location`; `X-Go-Azure-Sdk-Skip-Polling-Delay: true` present; status is never 202 | T2 | Slice 2. The header is global (ADR-0006 `AzureHeaders`) |

### Api

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| APIM-T22 | Create an API with `path`, `displayName`, `protocols`, `serviceUrl` | `PUT …/apis/{id}` => 201; `type=Microsoft.ApiManagement/service/apis`; `authenticationSettings{oAuth2AuthenticationSettings:[], openidAuthenticationSettings:[]}`, `subscriptionKeyParameterNames`, `isCurrent=true` | T1/T2 | Slice 2. Defaults are what azurerm dereferences; see provider notes |
| APIM-T23 | Address `apis/{id};rev=1` raw and as `%3Brev%3D1` | both resolve to one resource; `apiRevision="1"`; returned `id` keeps `;rev=1` | T1/T2 | Slice 2. Hook unit test plus an armtest Sample using a `;rev=` name |
| APIM-T24 | Update an API with PUT: `If-Match: *`, a stale ETag, and no header | `*` and no header => 200 (spec: optional on PUT). Stale ETag => 200 in miniblue | T2 | Slice 2. ADR-0003: `If-Match` is ignored and never required. Real APIM returns 412 on a stale ETag (verify); the lenience is deliberate |
| APIM-T25 | Create an API without `path`, or without `displayName` | `PUT` => 400 | T2 | Slice 2. Spec requires only `path` (min length 0, so an empty path is valid); `displayName` required on create is verify |
| APIM-T26 | PATCH `displayName`, `description`, `subscriptionRequired` under `properties` | `PATCH` => 200, merged document, other fields untouched | T1/T2 | Slice 2 (`PatchMerge`). The spec marks `If-Match` required on PATCH; miniblue accepts it absent |
| APIM-T27 | HEAD returns an ETag; PATCH with a matching ETag; PATCH with a stale ETag | HEAD => 200 with `ETag`; match => 200; stale => 412 `PreconditionFailed` (verify) | T2 | Deferred. No ETag or HEAD seam in ADR-0003. Optional for azurerm |
| APIM-T28 | GET an unknown API; any verb on an API under an unknown service | 404 `ResourceNotFound`; 404 `ParentResourceNotFound` (verify real APIM code) | T1 | Slice 2. armtest GetMissing and ParentMissing |
| APIM-T29 | List APIs, including on an empty service | `{value:[…]}`; empty service => `{"value":[]}` | T1 | Slice 2. azurerm lists and deletes APIs right after service create |
| APIM-T30 | DELETE an API with no `If-Match` and no query, then GET, then DELETE again | 200 (existed) or 204, then 404, then 204 | T1/T2 | Slice 2. Guard for `terraform destroy`. The spec lists 202 and 204 for this DELETE and the SDK accepts 200 and 204 (verify) |
| APIM-T31 | Two revisions `x;rev=1` and `x;rev=2`; list revisions; DELETE with `deleteRevisions` true and false | `GET apis/x/revisions` lists both; `DELETE …;rev=2` removes that revision only; `deleteRevisions` semantics verify | T2 | Follow-on. The revisions collection needs a list override or `Actions`; not in Slice 2 |
| APIM-T32 | `Location` header on API write responses | no `Location` header at all, so azurerm does not start its custom poller | T2 | Slice 2. See provider notes (polling) |

### Operation, product, backend, policy, named value

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| APIM-T33 | CRUD and list of operations under an API; missing required field; missing parent API | `displayName`, `method`, `urlTemplate` required (spec); missing => 400; missing parent => 404 `ParentResourceNotFound` | T1/T2 | Slice 2. Name 1 to 80, `displayName` max 300, `urlTemplate` max 1000 (spec) |
| APIM-T34 | Product CRUD; `state` honoured | `PUT` => 201; `state` `published` stays `published`, default `notPublished`; `displayName` 1 to 300, `description` max 1000; `subscriptionRequired`, `approvalRequired`, `subscriptionsLimit` echoed | T2 | Slice 2. Spec `state` enum is `notPublished`, `published`. `approvalRequired` without `subscriptionRequired` => 400 (verify) |
| APIM-T35 | Product HEAD returns an ETag; PATCH match and mismatch | HEAD => 200 with `ETag`; mismatch => 412 (verify) | T2 | Deferred, same reason as the API ETag row |
| APIM-T36 | Update a product with PUT and no `If-Match` | => 200 | T1/T2 | Slice 2. Spec: `If-Match` is optional on PUT |
| APIM-T37 | DELETE a product with `?deleteSubscriptions=true` and no `If-Match` | => 200 or 204; GET => 404 | T2 | Slice 2. Exactly what azurerm sends. The spec marks `If-Match` required on DELETE; azurerm omits it (verify the service accepts that) |
| APIM-T38 | Assign an API to a product | `PUT products/{p}/apis/{a}` => 201 (new) or 200 (existing); list under the product returns API contracts; `DELETE` removes; `HEAD` => 204; unknown product or API => 404 | T2 | `HEAD` deferred (no HEAD op). `products/apis` echoes its body in Slice 2 |
| APIM-T39 | Backend CRUD with `url`, `protocol`, `title`, `description`, `credentials`, `proxy`, `tls`, `circuitBreaker`; update by PUT without `If-Match` | PUT => 201 then 200; `protocol` in `http`, `soap`; `url` max 2000; fields echoed | T2 | Slice 2. Spec leaves `url` and `protocol` optional (Pool backends exist); require them for single backends (verify) |
| APIM-T40 | `POST backends/{b}/reconnect` | => 202 (spec); unknown backend => 404 | T2 | Deferred. 202 conflicts with sync-only (ADR-0006); azurerm never calls it. Decide 200, 204 or 202 when `Actions` has a consumer |
| APIM-T41 | Backend secret fields (`proxy.password`, `credentials.*`) | stored and returned in clear text; real Azure masks some (verify) | T2 | Documented drift, no `SecretPaths` seam |
| APIM-T42 | Service-level policy | `PUT …/policies/policy` `{properties:{format:"xml", value:"<policies>…</policies>"}}` => 201 or 200; GET returns `format` and `value`; missing `value` => 400; list works; absent => 404; `HEAD` returns an ETag; ETag changes on update | T2 | Tier B (azurerm reads `GET policies/policy?format=xml`, tolerates 404). Spec `format` enum: `xml`, `xml-link`, `rawxml`, `rawxml-link`. ETag and format conversion deferred |
| APIM-T43 | Named value create and read; secret handling; delete | `PUT` `displayName` plus `value` => 201; `secret=true` GET omits `value` (optional); DELETE without `If-Match` => 200 or 204 | T2/T3 | Slice 2 except masking. Spec: `displayName` 1 to 256 `^[A-Za-z0-9-._]+$`, `value` max 4096, name `^[^*#&+:<>?]+$`. Masking has no seam (verify real behaviour) |

### Portal settings, tenant access, subscriptions

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| APIM-T44 | Portal settings before any PUT; update; delegation secrets | `GET portalsettings/{signin,signup,delegation}` => 200 with `properties`; `PUT` => 200 (the only success code in the spec); `POST delegation/listSecrets` => 200 `validationKey` | T2 | Tier B. Needs a seeded-singleton seam and `Actions` |
| APIM-T45 | Tenant access before any PUT; update; secrets | `GET tenant/access` => 200 `{id:"access", properties{enabled,…}}`; `PATCH` `enabled` => 200; `POST listSecrets` => 200 `{id, enabled, primaryKey, secondaryKey}`; regenerate => 204 | T2 | Tier B. Same seams as above |
| APIM-T46 | Subscriptions | `PUT` => 201 or 200; GET, list, DELETE; `POST listSecrets` => 200 `{primaryKey, secondaryKey}`; `master` subscription exists | T2 | Follow-on. Spec requires `scope` and `displayName` on create |

### Terraform and lifecycle

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| APIM-T47 | azurerm: Consumption service, api, operation, product, named value, backend | apply; `plan -detailed-exitcode` is 0; second apply is a no-op; destroy is clean | T3 | Slice 2 (opt-in `tests/terraform/azurerm/apim_consumption`, PR 2.5). Fixture sets short `timeouts` |
| APIM-T48 | azapi: `Microsoft.ApiManagement/service@2024-05-01` plus child `apis@2022-08-01`, update tags with `azapi_update_resource` | apply, no diff, destroy; `response_export_values` returns the derived `gatewayUrl` | T3 | Slice 2 (opt-in azapi fixture). azapi treats 200 or 201 without async headers as complete |
| APIM-T49 | azurerm rejects an invalid service name before any HTTP call | `terraform validate` or `plan` exits non-zero in seconds | T3 | Opt-in. Exercises provider schema validation, not miniblue; server-side validation is the name-validation scenario above |
| APIM-T50 | DELETE the service removes every child (apis, operations, products, backends, namedValues, singletons); recreate has none | children GET => 404 after delete; a recreated service lists empty collections | T1 | Slice 2. armtest ChildCascade |
| APIM-T51 | List shape and paging | `{value}` with optional `count` and no `nextLink`; later `$top`, `$skip`, `$filter=contains(properties/displayName,'x')` | T2 | Deferred until paging lands; real lists return `{value, count, nextLink}` |

## Cost-tier test plan

Real APIM provisioning is never run from this repo, CI or an agent: Developer, Basic and Standard take 30 to 60+ minutes to create (Premium and multi-region longer, delete and purge also slow), and even Consumption takes minutes. azurerm's own timeouts are 3 h for service create, update and delete and 30 min for children. miniblue answers instantly, so the only cost is the harness.

| Tier | What runs | Where | Cost | Cadence |
|---|---|---|---|---|
| Default CI | armtest Samples for all 7 Slice 2 types (`Run`, `Coverage`, ChildCascade, ListSubscription); hook unit tests; httptest scenarios tagged T1 or T2 above | `tests/armsamples_apim_test.go`, `internal/services/apim/*_test.go`, `tests/arm_apim_test.go` | seconds, in-process, under 3 s for the APIM set | every PR |
| Opt-in fixture | azurerm `apim_consumption`; azapi `apim_service_api`; the negative `terraform validate` case | `tests/terraform/azurerm/apim_consumption`, `tests/terraform/azapi/apim_service_api`, one matrix entry each | about 1 to 2 min, dominated by `terraform init` and provider download; `run.sh` step limit is 600 s (`run.sh:107`) and the job limit 15 min (`acceptance-azurerm.yml:23`) | on demand; not in default CI (Slice 2) |
| Nightly | `apim_developer` (`Developer_1`, `tenant_access`, `sign_in`); `apim_purge` once soft-delete exists; latest-provider run | same tree | longer, more endpoints, higher drift risk | scheduled, never PR-blocking |
| Never | real-Azure provisioning, VNet, zonal or multi-region, `az apim` against a live service | none | 30 to 60+ min and real cost | documentation only |

Fixture rules:

- Every APIM fixture sets `timeouts { create = "5m" update = "5m" delete = "5m" read = "2m" }` on the service and children. A hung or 202-without-status response then fails in minutes instead of waiting out the 3 h provider default. The 10 min step limit and 15 min job limit remain the backstop.
- `run.sh` already exports `GO_AZURE_SDK_SKIP_POLLING_DELAY=true` (`run.sh:102`) and runs apply, `plan -detailed-exitcode` (must be 0), a second apply and destroy (`run.sh:117`). The global skip-polling header (ADR-0006) protects users who run the provider directly.
- Pin the provider exactly, as existing fixtures do (azurerm `= 5.10.0`, azapi `= 2.13.0`), and commit `.terraform.lock.hcl`: `run.sh` refuses a fixture without one (`run.sh:29`).
- Test data: subscription `00000000-0000-0000-0000-000000000000`, RG `apim-rg`, location `westeurope`, service names `apimtest-<n>`, publisher `Tests` and `admin@example.test`.

## Pitfalls

- **Never require `If-Match`.** The spec marks it optional on PUT and required on PATCH and DELETE, but azurerm sends `*` only on API create and update and sends nothing on any DELETE. Requiring it on update or delete breaks `terraform destroy` and in-place updates. A stale ETag is 412 on real APIM (verify); ignoring it is the deliberate lenient choice (ADR-0003).
- **Tests that assert rejection of an update PUT without `If-Match` encode a wrong behaviour.** Do not port them.
- **`checkNameAvailability` for an invalid name is `nameAvailable:false`, `reason:"Invalid"`.** An inverted validity check returning `true` is wrong.
- **Product `state` is honoured.** `published` stays `published`; the default is `notPublished`. Forcing it breaks `azurerm_api_management_product` with `published = true`.
- **Properties live under `properties`.** PATCH merges onto the stored document. Reading fields from the JSON root, or persisting the raw request instead of the merged document, loses data.
- **No async signals.** A write response carrying `Location` with an `asyncId` query parameter makes azurerm start a custom poller. DELETE returns 200 or 204 only: 201 or 202 routes azurerm to a custom poller that needs an absolute URL. A bare 202 fails with "unexpected status 202 received with no body" (ADR-0006).
- **A malformed `Location` such as `?asyncCode=…=asyncId=…` is worse than none.** Emit nothing.
- **Real `service` GETs are fully populated.** `gatewayUrl`, `gatewayRegionalUrl`, `portalUrl`, `developerPortalUrl`, `managementApiUrl`, `scmUrl`, `publicIPAddresses`, `platformVersion` and `identity` are all returned, and azapi `response_export_values` and azurerm outputs read them. Hard-coding only `provisioningState` is not enough.
- **`createdAtUtc` cannot be stable without the stored document.** Do not emit it unless it is copied from `Current` (Slice 2 emits none).
- **Soft-delete.** Real DELETE of a service soft-deletes it for a retention window (about 48 h, verify) and keeps the name reserved; `deletedservices` GET then returns 200. Name uniqueness is DNS-global (`{name}.azure-api.net`), across subscriptions. miniblue is subscription-local and allows immediate recreate.
- **`apis/{id};rev=N` is a distinct address per revision.** Real APIM also aliases `apis/{id}` to the current revision. azurerm always uses `;rev=1`; CLI and bicep may use the bare name (out of scope in Slice 2).
- **sku.name enum in 2024-05-01 is `Developer`, `Standard`, `Premium`, `Basic`, `Consumption`, `Isolated`, `BasicV2`, `StandardV2`.** `PremiumV2` is not in it. `sku.capacity` is required (Consumption 0, Developer 1; others verify).
- **Do not seed APIs or products.** azurerm lists and deletes them right after create. Real APIM does seed system groups (`administrators`, `developers`, `guests`), a `master` subscription and admin user 1; add those only when the group and subscription types land.
- **Real lists are `{value, count, nextLink}`.** Clients treat an empty or missing `nextLink` as done, so returning all items is safe.
- **Secrets are masked on real GET** for secret named values, subscription keys and similar fields, and served through `listValue` or `listSecrets` (verify per field). Echoing them in clear text is a documented deviation, not Azure behaviour.

## Dropped

| ID | Reason |
|---|---|
| APIM-T16 | ARM-template deployment of an APIM resource: depends on deployments dispatching in-process (ADR-0007 wave 3) and adds no APIM-specific assertion beyond the service lifecycle rows. Revisit with the deployments work |
