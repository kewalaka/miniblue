# Slice 2: APIM Consumption control plane

Proves:

- a new RP costs only registration, hooks and samples;
- `Current`-aware Defaults (stable identity and URLs);
- `PatchMerge`;
- parent existence;
- subscription-wide list;
- `Provisioned`;
- opaque `;rev=` names;
- the skip-polling header.

No bespoke chi routes are needed.

Depends on: PLAN phase 1. It is independent of Slice 1.

## Types

```go
// internal/services/apim/types.go
func Register(reg *arm.Registry) {
    reg.Register(arm.Type{
        Name:        "Microsoft.ApiManagement/service",
        APIVersions: []string{"2024-05-01", "2022-08-01"},
        Scopes:      arm.ScopeResourceGroup,
        Provisioned: true,
        Hooks:       arm.Hooks{Validate: validateService, Defaults: defaultService},
        Children: []arm.Type{
            {Name: "apis", Hooks: apiHooks, Children: []arm.Type{{Name: "operations", Hooks: operationHooks}}},
            {Name: "products", Hooks: productHooks, Children: []arm.Type{{Name: "apis"}}},
            {Name: "namedValues", Hooks: namedValueHooks},
            {Name: "backends", Hooks: backendHooks},
        },
    })
}
```

- Children default to `PatchMerge` (the zero value). That is harmless, and right for azapi.
- `products/apis` echoes its body.

## Hooks

| Type | Validate | Defaults |
|---|---|---|
| service | `CheckName(^[a-zA-Z][a-zA-Z0-9-]*[a-zA-Z0-9]$, 1, 50)`; `location`; `sku.name` in the known set; `sku.capacity` (Consumption 0); `publisherEmail` and `publisherName` required, max 100 (verify) | `customProperties` `{}` when absent (azurerm dereferences it without a nil check); `virtualNetworkType=None`; `publicNetworkAccess=Enabled`; `disableGateway=false`; `platformVersion=stv2`; `notificationSenderEmail`; URLs derived from the name (`gatewayUrl`, `gatewayRegionalUrl`, `managementApiUrl`, `scmUrl`, `portalUrl`, `developerPortalUrl`); `DefaultIdentity(doc, cur, uami)` |
| apis | `path` required, max 400; `displayName` required, max 300; name part before `;rev=` 1 to 256, `^[^*#&+:<>?]+$` | `authenticationSettings` `{oAuth2AuthenticationSettings:[], openidAuthenticationSettings:[]}`; `subscriptionKeyParameterNames {header:"Ocp-Apim-Subscription-Key", query:"subscription-key"}`; `type=http`; `protocols=["https"]` when absent; `subscriptionRequired=true`; `apiRevision` from `;rev=N` (default `"1"`); `isCurrent=true`; `isOnline=true` |
| operations | `displayName`, `method` and `urlTemplate` required; name 1 to 80 | echo |
| products | `displayName` required, max 300; `approvalRequired` only with `subscriptionRequired` | `state=notPublished`; `subscriptionRequired=true` |
| namedValues | `displayName` 1 to 256, `^[A-Za-z0-9-._]+$`; exactly one of `value` and `keyVault` | `secret=false`; `tags=[]` |
| backends | `url` required; `protocol` in {http, soap} | echo `credentials`, `proxy`, `tls`, `circuitBreaker` |

Validation errors are 400 `ValidationError` with `target` set to the property path (verify the code against real APIM).

Rules:

- Never emit `createdAtUtc`.
- Never emit `hostnameConfigurations` unless the client sent it.
- Never seed APIs or products. azurerm lists and deletes them right after create.

## Client behaviours to honour

- **azurerm service create:**
    - GET `locations/{loc}/deletedservices/{n}`: strict 404 is enough, so no route is needed.
    - PUT, then LIST `apis` and `products`. Both must return `{"value":[]}`.
- **azurerm service update is PATCH** (sku, properties, identity, tags), so it needs `PatchMerge`.
- **azurerm api paths are always `apis/{name};rev=1` with `If-Match: *`.**
    - The name is opaque: key on the decoded segment, echo it in `id` and `name`.
    - armtest covers both raw `;` and `%3B`.
    - Never require `If-Match`.
- **Never send a `Location` header** (an `asyncId` one triggers azurerm's custom poller), and never return 202.
- **namedValue PUT goes through `CreateOrUpdateThenPoll`.** The skip-polling header (ADR-0006) avoids a 10 s wait.

## Out of scope (documented deviations)

- `checkNameAvailability`.
- Soft-delete, `deletedservices` and purge: `purge_soft_delete_on_destroy=true` is unsupported.
- Tier B SKUs: portalsettings, tenant access, `listSecrets`.
- Subscriptions, users, groups, policies, loggers, diagnostics, certificates, gateways.
- `apis/{name}` to `;rev=<current>` aliasing.
- Secret masking.
- `$filter` and paging.
- Gateway runtime.

Each of these is a later PR. `Actions` lands with the first one that needs it (`listSecrets`).

## Acceptance

- **armtest:** Samples for all 7 types. `Coverage` passes. ChildCascade on service delete. RGCascade. ListSubscription for service.
- **Unit tests** (`internal/services/apim`):
    - Defaults idempotency: identity `principalId` stable across PUTs;
    - URL derivation;
    - `;rev=` parsing;
    - PATCH sku change preserves properties.
- **Terraform fixtures** (opt-in, `tests/terraform/azurerm/apim_consumption`, not in default CI):
    - `azurerm_api_management` (Consumption_0), `_api`, `_api_operation`, `_product`, `_named_value`, `_backend`.
    - apply, then a second plan is empty, then destroy is clean.
    - A matching `azapi_resource` fixture for service plus api, with `azapi_update_resource` on tags.
- Startup time and memory are unchanged within noise.

## PRs

1. `feat(apim): consumption control plane via arm engine` (types, hooks, samples, unit tests)
2. `test(apim): terraform azurerm and azapi consumption fixtures`
