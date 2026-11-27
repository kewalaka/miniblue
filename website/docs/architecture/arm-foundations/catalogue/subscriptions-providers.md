# Subscriptions, locations and providers

> Engine fit is engine gap for subscriptions and locations (singleton plus a subscription list) and engine gap for providers (provider gate and registry-derived list), with no passthrough types. miniblue today has a hand-written service `internal/services/subscriptions`. Suggested wave is PLAN 3.3 (subscriptions move onto the engine alongside resource groups) and PLAN 3.4 (`/providers` derived from the registry).

Subscriptions are read-only in real ARM at `/subscriptions/{id}`. Creating, cancelling and renaming subscriptions belongs to the separate `Microsoft.Subscription` provider and is out of scope for a single-subscription emulator, so those scenarios are in [Dropped](#dropped).

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `subscriptions` (get, list) | spec repo has 2022-12-01 (latest stable in the subscriptions spec) | tenant | locations, tagNames | `GET /subscriptions`, `GET /subscriptions/{id}`, `GET /tenants` | engine gap | subscription store behind `ScopeExists` (existence check is optional, see Pitfalls); read-only singleton type | M | S |
| `subscriptions/locations` | 2022-12-01 | subscription | none | `GET /subscriptions/{id}/locations` | engine gap | static location catalogue; read-only list | M | S |
| `subscriptions/tagNames` and `tagNames/tagValues` | resources spec 2025-04-01 `(verify)` | subscription | tagValues | `PUT tagNames/{n}` (200/201), `PUT tagNames/{n}/tagValues/{v}` (200/201), `DELETE` (200/204), `GET tagNames` | bespoke | none; tag counts derive from stored resources `(verify)` | L | S |
| `Microsoft.Resources/providers` | resources spec 2025-04-01 `(verify)` | tenant, subscription | resourceTypes | `GET /providers`, `GET /subscriptions/{s}/providers[/{ns}]`, `POST .../{ns}/register`, `POST .../{ns}/unregister`, `GET .../{ns}/resourceTypes`, `GET .../{ns}/providerPermissions` | engine gap | registry-derived list (PLAN 3.4), provider gate before the engine's existence phase, per-subscription registration state | M | S |

Real facts confirmed in the REST specs:

- `Subscription` carries `id`, `subscriptionId`, `displayName`, `tenantId`, `state` (`Enabled`, `Warned`, `PastDue`, `Disabled`, `Deleted`), `subscriptionPolicies` (`locationPlacementId`, `quotaId`, `spendingLimit`), `authorizationSource`, `managedByTenants`, and `tags`.
- `Location` carries `id`, `subscriptionId`, `name`, `type`, `displayName`, `regionalDisplayName`, `metadata`, and `availabilityZoneMappings`.
- `Provider` carries `namespace`, `registrationState`, `registrationPolicy`, `resourceTypes[]`, and `providerAuthorizationConsentState`. Each resource type has `resourceType`, `locations`, `apiVersions`, `defaultApiVersion`, and `capabilities`.
- The 2022-12-01 subscriptions spec has no create, PATCH or DELETE on `/subscriptions/{id}`.

## miniblue gaps

Ordered by value.

- Providers are static. There are 17 namespaces, always `Registered`, with no `registrationPolicy`, and only Storage, KeyVault and DocumentDB have `resourceTypes`; the rest return `[]`. There is no unregister, no tenant-level `GET /providers`, and no `.../resourceTypes`. The provider-entry `id` is hard-coded to the default subscription. PLAN 3.4 derives the list from the engine registry and merges with the static list until legacy types register.
- There is no provider gate. Nothing returns 409 `MissingSubscriptionRegistration`, because nothing can be unregistered.
- `/subscriptions/{id}/locations` is missing. Several SDKs and `az account list-locations` call it.
- Any subscription id is `Enabled`. `GET /subscriptions/{any}` returns the single default subscription's shape with the requested id. There is one fixed subscription (`00000000-0000-0000-0000-000000000000`, display name `miniblue`, tenant `...0001`).
- The subscription body is thin. `authorizationSource` is `Legacy` and `subscriptionPolicies` is absent.
- `GET /tenants` is served, while `tagNames` is not.
- Existing coverage includes `internal/services/subscriptions/handler_test.go`, `tests/subscriptions_test.go`, and `tests/subscriptionlist_test.go` (list by subscription for vnet, nsg, publicip, and load balancer).

## Scenarios

### Subscriptions

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T9 | GET a subscription id that does not exist. | GET `/subscriptions/{unknown}` returns 404 `SubscriptionNotFound` `(verify)`. | T2 | Conditional on a subscription store. Today any id is `Enabled`. Making it strict would break clients that use an arbitrary GUID, so gate it behind an opt-in (see Pitfalls). |
| G3-T10 | GET the subscription returns `id=/subscriptions/{guid}`, `subscriptionId`, `displayName`, `tenantId`, `state=Enabled`, plus `subscriptionPolicies` and `authorizationSource`. | GET `/subscriptions/{id}` returns 200 with those fields. | T2/T4 | Client smoke is `az account show`. Add `subscriptionPolicies` (`locationPlacementId`, `quotaId`, `spendingLimit`); real default `quotaId` and placement values are `(verify)`. |
| G3-T11 | Locations list for a subscription contains `eastus` and `westeurope`, and every entry has non-null `id` and `name`. An unknown subscription fails. | GET `/subscriptions/{id}/locations` returns 200; an unknown subscription returns 404 `SubscriptionNotFound` `(verify)`. | T2 | Missing today. Small and valuable because SDK location validation and `az` use it. Entries carry `displayName` and `regionalDisplayName`. |
| G3-T13 | Create a tag name and a tag value, then read the counts. | PUT `/subscriptions/{s}/tagNames/{t}/tagValues/{v}` returns 200 or 201 with body `count{type:Total,value:1}`; an invalid name or value returns 400 `(verify)`; an unknown subscription returns 404 `(verify)`. | T2 | Low value. The spec lists 200 and 201 for both PUTs. DELETE returns 200 or 204. |

### Providers

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T18 | List providers for a subscription returns at least 10 namespaces including `Microsoft.KeyVault`, `Microsoft.Storage`, `Microsoft.ContainerRegistry`, `Microsoft.ServiceBus`, and `Microsoft.EventHub`, and every entry has `registrationState` and `registrationPolicy`. | GET `/subscriptions/{s}/providers` returns 200 and `value` meets those checks. | T2 | `registrationPolicy` is missing today. Namespace match is case-insensitive. |
| G3-T19 | GET one provider returns `namespace=Microsoft.KeyVault`, `registrationState=Registered`, and `registrationPolicy=RegistrationRequired`. | GET `/subscriptions/{s}/providers/Microsoft.KeyVault` returns 200. | T2 | Policy string value is `(verify)`. An unknown namespace returns 404 `(verify)` code. |
| G3-T20 | Unregister `Microsoft.Storage`, then GET and list show `Unregistered`; register it again and both show `Registered`. | POST `.../unregister` returns 200; POST `.../register` returns 200. | T2 | Unregister is missing today. Real Azure passes through `Registering` and `Unregistering` states `(verify)`; the emulator may collapse them to the final state. Real unregister also fails while resources of that provider exist `(verify)`; do not port that check. |
| G3-T21 | Provider gate rejects PUT for a Key Vault while its namespace is unregistered for the subscription. The portal and `az` then auto-register and retry. | PUT `.../Microsoft.KeyVault/vaults/{n}` returns 409 `MissingSubscriptionRegistration`. | T2 | Gate only; the client auto-register half is client behaviour, not an emulator scenario. Default every namespace to `Registered` so existing flows are untouched; the gate fires only after an explicit unregister. Gate placement (middleware or an `Admitter`) is undecided. |
| G3-T22 | Tenant-level provider list returns every namespace with a non-empty `resourceTypes` array. | GET `/providers` returns 200. | T2 | Missing today. Only meaningful once PLAN 3.4 derives `resourceTypes` from the registry; until then only the registered types are non-empty, so restrict the assertion to those. |

## Pitfalls

- Subscription creation is not a `/subscriptions/{id}` operation. Real ARM exposes only GET, list, locations and tenants there; creating a subscription uses `Microsoft.Subscription` aliases, and cancel and enable are billing-owned POSTs under that provider. A PATCH or DELETE on `/subscriptions/{id}` has no real counterpart.
- A single fixed subscription is a feature for emulator users who set `ARM_SUBSCRIPTION_ID` to any GUID. Returning 404 `SubscriptionNotFound` for unknown ids is real, but must stay opt-in or it breaks those setups. Treat the strict check as conditional.
- A PUT or GET under an unknown subscription returns 404, not a server error.
- `GET /subscriptions` lists subscriptions the caller can see; with one fixed subscription it returns one. Do not assert multiple results.
- Provider registration is per subscription, not global, and state names are exact strings. A gate that rejects after registering (stale state) is a worse failure than no gate.
- `registrationPolicy` and `registrationState` are separate fields. The policy says whether registration is required, and the state says whether this subscription registered.
- Tag name and tag value PUT return 200 or 201 depending on whether the entry is new.

## Dropped

| ID | Reason |
|---|---|
| G3-T12 | List with multiple subscriptions needs a way to create or seed them. Create is not real ARM at this path (see the create row below) and no seeding mechanism is designed; the single fixed subscription is the supported model. |
| G3-T14 | Subscription tag update, value update and removal through a PATCH on `/subscriptions/{id}` is not a real ARM operation. |
| G3-T15 | Create (POST), delete and PATCH on `/subscriptions/{id}` are not real ARM; creation goes through `Microsoft.Subscription` aliases and is out of scope. |
| G3-T16 | Cancel and enable routes (`POST /subscriptions/{id}/providers/Microsoft.Subscription/cancel` and `enable`) belong to the `Microsoft.Subscription` provider and are billing-owned; they are out of scope for a fixed single subscription. |
| G3-T17 | Create validation of a malformed id or empty body does not apply because the create route is not real ARM (see the create, delete and PATCH row). |
