# API Management: provider notes

How the azurerm provider (and azapi) drive APIM, and what a miniblue response must contain for them to finish. This backs the scenarios in [API Management](api-management.md) and the hooks in [Slice 2](../slices/02-apim-consumption.md); it does not repeat the slice's defaults table or its short client-behaviour list.

Source: the azurerm `v5.10.0` and go-azure-sdk read made during the spike, spot-checked against azurerm `main` for the API resource (`If-Match`, `;rev=` path) and the service resource (deleted-service lookup, purge flag, `tenant_access` gating). Line-level details drift between releases, so each claim not re-checked is tagged `(verify)` against the release a fixture pins. Nothing here was run against Azure.

## azurerm_api_management (service)

Provider timeouts: create, update, delete 3 h; read 5 min.

| Step | Calls | miniblue must |
|---|---|---|
| Create | `GET service` (expects 404); `GET locations/{loc}/deletedservices/{n}`; `PUT service` | 404 on the first GET; 404 on the deleted-service GET (404 or 403 is tolerated; a 200 fails the create unless `recover_soft_deleted` is set); PUT => 201 with `provisioningState=Succeeded` |
| Create cleanup | `LIST apis`, `DELETE` each with `deleteRevisions=true`; `LIST products`, `DELETE` each with `deleteSubscriptions=true` | both lists return `{"value":[]}`, so `apis` and `products` must be registered even if nothing is seeded |
| Create, non-Consumption and non-V2 only | `PUT portalsettings/signin`, `PUT portalsettings/signup`; `PUT portalsettings/delegation` and `PATCH tenant/access` only when configured | Tier B routes (see below) |
| Update | `GET service`, then `PATCH` with `sku`, `tags`, `properties`, `identity` | `PatchMerge`; the PATCH response is 200 (the spec also allows 202, avoid it) |
| Read | `GET service`; `GET policies/policy?format=xml` (404 tolerated) | every field in the next table present or nil-safe |
| Delete | `GET service`, then `DELETE`; with `purge_soft_delete_on_destroy`, `GET` then `DELETE` on `deletedservices/{n}` | DELETE => 200 existed, 204 missing. Purge mode is unsupported in Slice 2 because the post-delete `GET deletedservices` must succeed |

The PUT body always carries `sku{name,capacity}`, `location`, `tags`, `identity` and `properties{publisherName, publisherEmail, publicNetworkAccess, customProperties, certificates, disableGateway, virtualNetworkType}`, plus `hostnameConfigurations`, `additionalLocations`, `notificationSenderEmail` and similar only when configured. azapi and bicep users often omit `customProperties`, so Defaults must add `{}`.

### What Read dereferences

| Read field | Why it must be present |
|---|---|
| `properties.customProperties` | dereferenced without a nil check `(verify)`; must be an object even when the client sent none. Client keys stay verbatim; missing keys read as false |
| `gatewayUrl`, `gatewayRegionalUrl`, `portalUrl`, `developerPortalUrl`, `managementApiUrl`, `scmUrl` | exported as attributes; azapi `response_export_values` reads them too. Exact URL shapes are verify; the provider copies strings |
| `publicIPAddresses`, `privateIPAddresses`, `notificationSenderEmail`, `minApiVersion`, `zones` | exported; nil-safe, but real Azure returns them |
| `hostnameConfigurations` | handled nil-safe and filtered for defaults; emit only what the client sent |
| `identity` | read back; `principalId` and `tenantId` must not change between PUTs (use `DefaultIdentity` with `Current`) |
| `sku` | exported as `Name_Capacity`; Consumption capacity is 0 |

## API, operation, product, backend, named value

| Resource | Create | Update | Read | Delete |
|---|---|---|---|---|
| `azurerm_api_management_api` (30 min) | `GET apis/{name};rev={rev}` (expects 404), then `PUT` same path with `If-Match: *` (non-import path) | `PUT` with `If-Match: *` (import variant sends none) | `GET apis/{name};rev=N` | `DELETE` with no `If-Match`; 404 tolerated |
| `_api_operation`, `_product`, `_backend` | plain `PUT`, accepts 200 or 201 | plain `PUT`, no `If-Match` | `GET`, nil-safe | `DELETE` with no `If-Match`; product adds `deleteSubscriptions=true` |
| `_named_value` | `PUT`, accepts 200, 201 or 202, treated as long-running | same | `GET`; does not read `value` when `secret=true` | `DELETE` with no `If-Match` |

- API revision defaults to `"1"`, so the path is always `…/apis/{name};rev=1`. The name segment is opaque: key on the decoded segment, echo it in `id` and `name`. `;` is kept raw by some clients and `%3B` encoded by others.
- API Read dereferences `authenticationSettings.oAuth2` and `.openid` through a pointer chain `(verify)`. Defaults must return `authenticationSettings` with both authentication-settings arrays empty. Also default `subscriptionKeyParameterNames`, `type`, `isCurrent`, `protocols` and `subscriptionRequired` (values in the slice's hooks table).
- API body fields azurerm sends: `displayName`, `path`, `protocols`, `serviceUrl`, `subscriptionRequired`, `apiRevision`, `apiRevisionDescription`, `apiVersion`, `apiVersionDescription`, `apiVersionSetId`, `subscriptionKeyParameterNames`, `authenticationSettings`, `contact`, `license`, `termsOfServiceUrl`, `type`, `description`, `sourceApiId`.
- Named value body: `{displayName, value or keyVault, secret, tags}`. A child with no `provisioningState` counts as succeeded.

Status codes the go-azure-sdk accepts:

| Operation | Accepted |
|---|---|
| service `PUT` | 200, 201 |
| service `PATCH` | 200, 202 |
| service `DELETE` | 200, 202, 204 |
| deleted-service purge | 200, 202, 204 |
| child `PUT` | 200, 201 (named value also 202) |
| child `DELETE` | 200, 204 |
| `portalsettings/{signin,signup,delegation}` `PUT` | 200 only |
| every `listSecrets` | 200 |

## Polling and headers

| Client path | Behaviour |
|---|---|
| go-azure-sdk, 200 or 201 with `Azure-AsyncOperation` or `Location` | long-running poller, initial wait is `Retry-After` or 10 s |
| go-azure-sdk, `PUT`, `PATCH` or `POST` returning 200 or 201 with a JSON body | provisioningState poller: re-`GET` after 10 s; `Succeeded` or absent is done; `Failed` or `Canceled` is an error |
| go-azure-sdk, `DELETE` 200, 201, 202 or 204 with no polling headers | poll `GET` until 404 (10 s initial) |
| azurerm service delete returning 201 or 202 | custom poller on `Azure-AsyncOperation` or `Location`, needs an **absolute** URL, 20 s interval `(verify)` |
| azurerm API `PUT` whose `Location` carries an `asyncId` query parameter | custom poller that re-`GET`s with `?asyncId=` until 200 `(verify)` |
| azapi and azcore | 200 or 201 without async headers is complete |

So: synchronous 200 or 201 with `provisioningState: Succeeded`, no `Azure-AsyncOperation`, no `Location`, never 202 (ADR-0006). The global `X-Go-Azure-Sdk-Skip-Polling-Delay: true` header removes the 10 s waits on service and named-value writes. The acceptance runner also sets `GO_AZURE_SDK_SKIP_POLLING_DELAY=true`, so CI is fast either way.

## Tier B reads (non-Consumption, non-V2 SKUs)

Read adds these calls, all of which must succeed without a prior PUT. Seed them in code (no stored document needed) once the seeding seam exists.

| Call | Response azurerm needs |
|---|---|
| `GET portalsettings/signin` | `properties.enabled` |
| `GET portalsettings/signup` | `properties.enabled`, `termsOfService{enabled, consentRequired}` |
| `GET portalsettings/delegation` | `properties{url, subscriptions{enabled}, userRegistration{enabled}}`; read even when never configured |
| `POST portalsettings/delegation/listSecrets` | `validationKey` |
| `GET tenant/access` | `id`, `properties{enabled, id}`; must succeed with no prior PUT |
| `POST tenant/access/listSecrets` | `id`, `enabled`, `primaryKey`, `secondaryKey`; the model is dereferenced, so all four fields |

`tenant_access` is rejected by the provider itself for Consumption and V2 SKUs, so Tier A never sees it. Portal-settings `PUT` returns 200 only; the SDK accepts no 201.

## azapi

- Create: `GET` (404 proceeds), `PUT` (200 or 201), `GET` read-back. Update: `GET`, merge, full `PUT`, `GET` (no PATCH). Delete tolerates 200, 202, 204 and 404.
- `response_export_values` returns whatever the service GET carries, so derived URLs and identity ids must be real strings.
- Schema validation is client-side, so an unknown type or version never reaches miniblue. Versions to exercise: 2024-05-01 for the service, 2022-08-01 for children, plus one preview such as 2023-05-01-preview to confirm any well-formed version is accepted (ADR-0005).
