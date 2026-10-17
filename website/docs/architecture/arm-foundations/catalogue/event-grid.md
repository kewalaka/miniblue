# Event Grid

> Engine fit: hooks for `topics`, `namespaces` and `systemTopics`; passthrough child for `eventSubscriptions` (plus a second, extension-scoped form); bespoke for the data plane and delivery. miniblue today: hand-written service `internal/services/eventgrid` (topics only, plus an accept-only publish route). Suggested wave: migration wave 2 ([PLAN](../PLAN.md) Phase 4, migrate-only, low value); `namespaces`, `systemTopics` and `eventSubscriptions` are new types after G2.

Control plane and data plane are separate. The data plane section covers only publish and a minimal webhook delivery; push-delivery retries, dead-lettering and pull delivery are out of scope.

## Types

Latest stable api-version is 2025-02-15 (confirmed in the REST spec repo); previews run up to 2026-06-15-preview. azurerm and azapi pins were not checked (verify). `APIVersions` is metadata only (ADR-0005), so any well-formed version is accepted.

Name rules from Learn: topics and domains 3 to 50 alphanumerics and hyphens, event subscriptions 3 to 64 alphanumerics and hyphens.

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.EventGrid/topics` | 2025-02-15 | RG | eventSubscriptions | `POST listKeys`; `POST regenerateKey` body `{keyName}`; list by subscription and by RG | hooks | `Type.Actions` (listKeys, regenerateKey); `Reader.Under` for the subscription-wide list; `PatchMode` merge; `Hooks.Defaults` for `endpoint`, `provisioningState`, `inputSchema`, `publicNetworkAccess` | M | S |
| `Microsoft.EventGrid/namespaces` | 2025-02-15 | RG | namespace topics and their event subscriptions (not in the source scenarios) | `POST listKeys`; `POST regenerateKey` body `{keyName}`; list by subscription and by RG | hooks | same as topics; `sku` is `Standard` with capacity 1 to 20 (spec) | M | S |
| `Microsoft.EventGrid/systemTopics` | 2025-02-15 | RG | eventSubscriptions | list by subscription and by RG | hooks | `Hooks.Validate` (`source` and `topicType` required, verify); `identity` supported | L | S |
| `topics/eventSubscriptions`, `systemTopics/eventSubscriptions` | 2025-02-15 | child of topics or systemTopics | none | `GET {parent}/eventSubscriptions` list; `POST getFullUrl`; `POST getDeliveryAttributes` | passthrough | `Children`; `Type.Actions` for both POSTs | M | S for CRUD, L with delivery |
| `Microsoft.EventGrid/eventSubscriptions` (extension form) | 2025-02-15 | subscription, RG and resource scope: `{scope}/providers/Microsoft.EventGrid/eventSubscriptions/{n}` | none | `POST getFullUrl`; `POST getDeliveryAttributes`; global, regional and per-topic-type lists | engine-gap | a `ScopeResource` type; ADR-0003 does not existence-check extension resources at resource scope | M | S |
| Data plane: `POST /api/events` on the topic endpoint | not versioned through ARM (verify) | per topic endpoint | none | event and CloudEvent schemas; validation-event handshake for webhook destinations | bespoke | background delivery worker; reads the stored subscription | M | M |

PUT, PATCH and DELETE are long-running in the real spec (topics: PUT 201 only, PATCH 200 or 201, DELETE 202 or 204). miniblue is sync (ADR-0006): PUT 201 on create and 200 on replace, PATCH 200, DELETE 200 or 204.

## miniblue gaps

Ordered by value. Line numbers refer to `internal/services/eventgrid/handler.go`.

- No `eventSubscriptions` in either form, so Bicep, Terraform and the SDK cannot create a subscription. No `getFullUrl` or `getDeliveryAttributes`.
- No `namespaces` or `systemTopics`.
- Topics have no `PATCH`, no subscription-wide list (the list route is RG-scoped only, lines 35-43) and no `regenerateKey`.
- `listKeys` returns two fixed strings (lines 115-116), so rotation can never be observed.
- `DELETE` returns a bare 202 with no `Location` or `Azure-AsyncOperation` header (line 96) and 404 when the topic is missing. ADR-0006 wants 200, or 204 when missing.
- `PUT` ignores body decode errors (line 56), so a null or malformed body creates a topic. No `location` or name check.
- The data plane route is `POST /eventgrid/{topicName}/events` (line 43). It only decodes a JSON array (line 120), never checks the topic exists, never delivers, and enforces no size limit.
- The topic `endpoint` is hard-coded to `https://<name>.eastus-1.eventgrid.azure.net/api/events` (line 61). That host does not resolve locally, so an SDK publisher built from the ARM response cannot reach miniblue.
- Store keys are case-sensitive concatenations (`topicKey`, `eg:topic:` prefix), so two spellings of the same id become two topics. Moving to the engine fixes this.

## Scenarios

`{t}` is a topic name, `{p}` is the path `/subscriptions/{s}/resourceGroups/{rg}/providers/Microsoft.EventGrid`. Codes and statuses follow ADR-0006 (sync).

### topics

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T34 | Creating a topic returns its endpoint and a succeeded state. | `PUT {p}/topics/{t}` body `{location}` => 201; `properties.endpoint` is `https://{t}.{region}-1.eventgrid.azure.net/api/events`; `properties.provisioningState` is `Succeeded`; a second `PUT` => 200 | T2 | Flag: the spec lists only 201 for `PUT` (LRO). 200 on replace is the ADR-0006 sync rule. Defaults (verify): `inputSchema` `EventGridSchema`, `publicNetworkAccess` `Enabled`, `minimumTlsVersionAllowed` `1.2`. |
| G2-T35 | Reading a topic returns what was created. | `GET {p}/topics/{t}` => 200, same body as the `PUT` response | T1/T2 | |
| G2-T36 | Reading a missing topic. | `GET {p}/topics/{t}` => 404 `ResourceNotFound` | T1 | Generic ARM not-found, covered by the `armtest` row once the type migrates. |
| G2-T37 | `PATCH` replaces the tags. | `PATCH {p}/topics/{t}` body `{tags}` => 200; tags are the new set; `properties` unchanged | T2 | Spec marks `PATCH` as LRO (200 or 201); sync 200 here. `PatchMode` merge also covers the patchable `properties` (`publicNetworkAccess`, `inboundIpRules`, `disableLocalAuth`; verify the update model). |
| G2-T38 | A deleted topic is gone. | `DELETE {p}/topics/{t}` => 200; `GET` => 404; `DELETE` again => 204 | T1/T2 | Flag: real `DELETE` is an LRO (202 or 204). Sync with no async headers. Tests against real Azure accept 202 or 204 and poll. |
| G2-T39 | Listing by resource group. | `GET {p}/topics` => 200 `{value:[...]}` containing the created topic | T1 | |
| G2-T40 | Listing by subscription. | `GET /subscriptions/{s}/providers/Microsoft.EventGrid/topics` => 200 `{value:[...]}` including topics from other resource groups | T1/T2 | Needs `Reader.Under`; ADR-0005 makes subscription-wide list part of the generic row for RG-scoped top-level types. |
| G2-T41 | `listKeys` returns two non-empty keys. | `POST {p}/topics/{t}/listKeys` => 200 `{key1, key2}`; the values are stable across calls; missing topic => 404 | T2 | Fixed placeholder strings today; generate random base64 keys at create time. |
| G2-T42 | `regenerateKey` rotates only the named key. | `POST {p}/topics/{t}/regenerateKey` body `{"keyName":"key1"}` => 200 `{key1, key2}`; `key1` changed, `key2` unchanged; `keyName` is required | T2 | Spec marks it 200 or 202 (LRO); sync 200 here. An unknown `keyName` is a 400 (verify code). |

### namespaces

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T43 | Namespaces follow the same lifecycle as topics. | `PUT` 201 then 200, `GET` 200, missing `GET` 404, `PATCH` tags 200, `DELETE` 200, list by RG and by subscription 200 for `{p}/namespaces/{n}` | T1/T2 | Mirrors G2-T34 to G2-T40. `sku` defaults to `Standard`; capacity range is 1 to 20 (spec). Properties include `topicsConfiguration`, `isZoneRedundant`, `publicNetworkAccess`, `minimumTlsVersionAllowed`. |
| G2-T44 | Namespaces have `listKeys` and `regenerateKey`. | `POST {p}/namespaces/{n}/listKeys` => 200 `{key1, key2}`; `POST .../regenerateKey` body `{keyName}` => 200 and the named key changes | T2 | Same shape as topics (spec confirms `key1`, `key2`, required `keyName`). |

### systemTopics

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T45 | System topics follow the same lifecycle. | CRUD, `PATCH`, list by RG and by subscription for `{p}/systemTopics/{n}`; `properties.source` and `properties.topicType` are required on create, a missing one => 400 | T2 | Mirrors G2-T34 to G2-T40. Required-ness of `source` and `topicType` is inferred (verify). Properties include `provisioningState`, `metricResourceId`; the resource supports `identity`. |

### eventSubscriptions

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T46 | Event subscriptions have a full lifecycle under a topic. | `PUT {p}/topics/{t}/eventSubscriptions/{e}` => 201 then 200; `GET` 200; `PATCH` 200; `GET {p}/topics/{t}/eventSubscriptions` => 200 `{value:[...]}`; `DELETE` => 200 then `GET` => 404; missing topic => 404 `ParentResourceNotFound` | T2/T3 | Serve both forms: the child path above and `{scope}/providers/Microsoft.EventGrid/eventSubscriptions/{e}` at subscription, RG and resource scope (the extension form, spec PUT 201). Which form azurerm and Bicep use is (verify). Name 3 to 64 alphanumerics and hyphens. In real Azure a webhook destination is validated at create (see G2-T48). |
| G2-T47 | `getFullUrl` and `getDeliveryAttributes` are POST actions on a subscription. | `POST .../eventSubscriptions/{e}/getFullUrl` => 200 `{endpointUrl}`; `POST .../getDeliveryAttributes` => 200 `{value:[{name, type, properties{value, isSecret}}]}` | T2 | Response shapes are inferred (verify). Both are `Type.Actions` on the child and the extension form. |

### Data plane (bespoke)

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T48 | Publishing to a topic endpoint delivers to a webhook subscription after a validation handshake. | `POST /api/events` with an event array => 200; a subscription whose destination is a local webhook first receives a `Microsoft.EventGrid.SubscriptionValidationEvent` carrying a `validationCode`, the endpoint answers with `validationResponse`, then the published event arrives | T2 | Bespoke delivery worker; the test starts its own webhook with `httptest.NewServer`. Real Azure requires a valid `aeg-sas-key` or AAD token (401 otherwise, verify); decide whether miniblue checks it. Retries and dead-lettering are out of scope. |
| G2-T49 | A very large batch of small events. | `POST /api/events` with more than 5000 events => 400 (verify) | T2 | Not confirmed: Learn documents no per-batch event count, only the 1 MB array limit (G2-T50). Do not implement until confirmed. |
| G2-T50 | An oversized payload is rejected. | `POST /api/events` with a body over 1 MB, single event or array => 413 | T2 | Limit is 1 MB per request per Learn; the exact error body is (verify). |

### Template deployment

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T51 | A template can create a topic with event subscription children. | deployment with `Microsoft.EventGrid/topics` and `Microsoft.EventGrid/topics/eventSubscriptions` succeeds; both resources exist afterwards | T2 | Cross-owner: depends on the deployments dispatch (PLAN 3.1, `ChildID` for nested ids) and on `eventSubscriptions` being registered. See [deployments](deployments.md). |

### Validation

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T52 | Bad create requests are rejected. | `PUT {p}/topics/{t}` with a null body, an empty body or no `location` => 400 (`LocationRequired` for a missing location, verify); a name outside 3 to 50 alphanumerics and hyphens => 400 `InvalidResourceName` (verify code) | T1/T2 | Body validation and `CheckName` (ADR-0003). Today decode errors are ignored (`handler.go:56`). The source's "missing name" case has no equivalent: an empty name segment is a different route. |

## Pitfalls

- Topic `PUT` is spec'd as 201 only, and `DELETE` is 202 or 204; both are long-running. miniblue returns 201 or 200 and 200 or 204 (ADR-0006). A 202 with no operation header leaves pollers without a status URL, so do not keep today's bare 202.
- Event subscriptions are extension resources in real clients. Serving only the child-of-topic form misses `{scope}/providers/Microsoft.EventGrid/eventSubscriptions/{n}` (verify which form azurerm and Bicep use). Resource-scope extension resources are not existence-checked by the engine (ADR-0003).
- Webhook destinations are validated during create. Real Azure fails the `PUT` when the endpoint does not answer the validation event (verify), so a subscription must not be marked `Succeeded` while the handshake is pending.
- `regenerateKey` is the Event Grid spelling with body `{keyName}` and `key1`/`key2` in the response. Event Hubs and Service Bus use `regenerateKeys` with `keyType`. Do not share one handler.
- Static keys hide rotation bugs. Rotate only the named key and leave the other untouched.
- The publish limit is 1 MB per request, with 413 above it. A per-batch event count of 5000 is not documented (verify).
- `inputSchema` (`EventGridSchema`, `CloudEventSchemaV1_0`, `CustomEventSchema`) decides the shape the data plane must accept; a topic created for CloudEvents rejects the Event Grid schema in real Azure (verify).
- Real publish endpoints are regional DNS names that do not resolve locally. Return an endpoint that reaches miniblue, or SDK publishers cannot be tested.
- Tag `PATCH` replaces the whole tag set rather than merging keys (verify).
- Ids are case-insensitive. Normalise on lookup and return canonical casing.

## Dropped

| ID | Reason |
|---|---|
| G2-T53 | Wrappers (CLI and MCP tools) over the same REST calls; not portable and no new behaviour. |
