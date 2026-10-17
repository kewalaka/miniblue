# Service Bus

> Engine fit: hooks (namespaces), passthrough children (authorizationRules, queues, topics, subscriptions, rules), engine-gap (`$Default` rule, `networkRuleSets/default`), bespoke (messaging data plane). miniblue today: hand-written service `internal/services/servicebus` (ARM namespaces, queues, topics; JSON data plane). Suggested wave: PLAN wave 4 for namespaces, queues, topics; subscriptions, rules and authorizationRules as new types after G2. The data plane never migrates.

Paths below are relative to `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.ServiceBus`. Spec facts come from `Microsoft.ServiceBus/ServiceBus` in the Azure REST API specs (latest stable `2026-01-01`, latest preview `2026-07-01-preview`).

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `namespaces` | real latest stable `2026-01-01`; azurerm pin `(verify)` | RG; list by RG and subscription | authorizationRules, queues, topics, `networkRuleSets/default` | PATCH; `checkNameAvailability` POST (chi route) | hooks | `Hooks.Defaults` (endpoint, metricId, timestamps, `RootManageSharedAccessKey`), `PatchMerge`, subscription-wide list (`Under`), global-unique name has no seam | H | M |
| `namespaces\|queues\|topics/authorizationRules` | same (spec path uses `AuthorizationRules`; ARM paths are case-insensitive) | namespace, queue, topic | none | `listKeys`, `regenerateKeys` (POST, 200) | passthrough child + actions | `Type.Actions` (lands with the first `listKeys` consumer), key generation in an action | H | M |
| `namespaces/queues` | same | namespace | authorizationRules | none | passthrough + `Defaults` | `Hooks.Defaults` for the property defaults | M | S |
| `namespaces/topics` | same | namespace | subscriptions, authorizationRules | none | passthrough + `Defaults` | `Hooks.Defaults`, `DeleteTree` for the cascade | M | S |
| `topics/subscriptions` | same | topic | rules | none (PUT, GET, DELETE, list) | passthrough child + auto-child | child type; `$Default` rule seed has no seam | H | S-M |
| `subscriptions/rules` | same | subscription | none | none | passthrough child | child type | H | S |
| `namespaces/networkRuleSets/default` | same | namespace | none | GET, PUT only; no DELETE | engine-gap: singleton | no seam: bespoke GET with defaults | H (azurerm reads it) | S |
| Data plane: queue and topic messaging, dead-letter, forwarding, sessions | n/a | namespace endpoint | none | send, receive, publish | bespoke | none; stays chi | M | L |

## miniblue gaps

Ordered by value.

- No `subscriptions`, `rules`, `authorizationRules`, `listKeys` or `regenerateKeys`. `azurerm_servicebus_subscription` and `azurerm_servicebus_namespace` (connection strings) cannot work.
- No ARM DELETE for subscriptions, which `azurerm_servicebus_subscription` destroy needs. Real Azure has it (spec: 200/204).
- No `networkRuleSets/default`, no list by subscription, no PATCH.
- Namespace responses are fixed: location `eastus`, hard-coded `createdAt`/`updatedAt` (`handler.go:220`, `handler.go:230-231`). Queue and topic ARM PUT also build a fixed body (`handler.go:311-325`), so request body properties and tags are dropped.
- Namespace DELETE removes only data-plane keys (`sb:queue:`, `sb:topic:`) (`handler.go:277-278`). ARM queues and topics (`sb:armqueue:`, `sb:armtopic:`; `handler.go:292`, `handler.go:367`) survive and reappear if the namespace is recreated.
- Namespace DELETE of an existing namespace returns 202 with no `Location` (`handler.go:281`); a missing one returns 404 (real: 204).
- ARM queues and topics are a separate key space from the data-plane queues and topics, so an entity created over ARM is not the same object the data plane sends to.
- The RG cascade covers `sb:namespace:`, `sb:armqueue:`, `sb:armtopic:` (`resourcegroups/handler.go:192-194`) but not data-plane keys or any new type.

## Scenarios

### Namespaces

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T78 | Namespace body carries service-generated fields; empty list is `{"value":[]}` | `PUT`/`GET namespaces/{n}` => `serviceBusEndpoint`, `metricId`, `disableLocalAuth`, `minimumTlsVersion`, `publicNetworkAccess`, `provisioningState` `Succeeded`, `createdAt`, `updatedAt`; `GET list` with none => `{"value":[]}` | T2 | Spec field names are `createdAt`/`updatedAt` (not `createdOn`/`updatedOn`). `status` is also read-only in the spec |
| G3-T79 | List by subscription spans resource groups | `GET /subscriptions/{sub}/providers/Microsoft.ServiceBus/namespaces` => 200, includes namespaces from two RGs | T1/T2 | Engine: `Under` subscription-wide list. Missing in miniblue |
| G3-T80 | Deleting a namespace removes every child and frees the name | `DELETE namespaces/{n}` => 200; `GET` queue, topic, subscription, rule, authorization rule => 404; recreating the namespace shows no children; `DELETE` missing => 204 | T2 | Spec lists 200/202/204. miniblue orphans ARM queues and topics today |
| G3-T81 | Namespace names are globally unique | second `PUT` of the same name in another RG or subscription => 409 (code `(verify)`); after `DELETE`, `PUT` => 201 | T2 | Engine-gap: global-unique |
| G3-T82 | PUT in a missing resource group fails; `sku` is required | `PUT` in unknown RG => 404 `ResourceGroupNotFound`; `PUT` without `sku` => 400 (code `(verify)`) | T1/T2 | Spec requires `sku.name`; whether `sku` itself is required on the namespace is `(verify)` |

### Authorization rules

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T83 | A new namespace has `RootManageSharedAccessKey`; `listKeys` returns keys and connection strings | after `PUT` namespace, `GET authorizationRules/RootManageSharedAccessKey` => `rights` Listen, Manage, Send; `POST .../listKeys` => 200 with `keyName`, `primaryKey`, `secondaryKey`, `primaryConnectionString` (contains `SharedAccessKey=`), `secondaryConnectionString`, `aliasPrimaryConnectionString`, `aliasSecondaryConnectionString` | T2/T3 | Spec marks all `listKeys` fields read-only. Alias fields are empty without geo-DR `(verify)`. Needs `Type.Actions` and an auto-child on namespace create |
| G3-T84 | Authorization rules on namespace, queue and topic; `regenerateKeys` rotates one key | `PUT` with `rights` => 200; `GET`, `GET list`, `DELETE` (200, missing 204); `POST regenerateKeys` `{"keyType":"PrimaryKey"}` => 200, only `primaryKey` changes; `SecondaryKey` changes only `secondaryKey` | T2 | Body also accepts an optional `key` to set the value. `rights` enum is Listen, Send, Manage |

### Queues

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T85 | Queue defaults and update | `PUT queues/{q}` => 200 or 201; `lockDuration` `PT1M`, `maxSizeInMegabytes` 1024, `maxDeliveryCount` 10, `duplicateDetectionHistoryTimeWindow` `PT10M`, `requiresSession` false, `deadLetteringOnMessageExpiration` false, `defaultMessageTimeToLive` and `autoDeleteOnIdle` at the maximum duration; a second `PUT` with changes is reflected on `GET` | T2 | Defaults and the maximum-duration string are `(verify)`. Spec lists 200 for PUT, see the new-case row. Hook: `Defaults` |
| G3-T86 | Queue forwarding properties round-trip; delete; list | `PUT` with `forwardTo` and `forwardDeadLetterMessagesTo` => `GET` returns them; `DELETE` => 200, missing 204; `GET list` incl. empty `{"value":[]}` | T2 | ARM part only; the messaging behaviour is in the data-plane forwarding row |

### Topics

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T87 | Topic CRUD, update, list; delete cascades subscriptions | `PUT`/`GET`/`DELETE topics/{t}`; update reflected on `GET`; `GET list` incl. empty; `DELETE topic` then `GET` its subscriptions and rules => 404 | T2 | Needs `DeleteTree` for the cascade |

### Subscriptions

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T88 | Subscription create is idempotent; defaults; missing is 404 | `PUT topics/{t}/subscriptions/{s}` => 200 or 201 (twice is not an error); `GET` => `maxDeliveryCount` 10, `deadLetteringOnFilterEvaluationExceptions` true; `GET` missing => 404 `ResourceNotFound` | T2 | Missing in miniblue. Not-found code is the generic one, not a per-entity code `(verify)` |
| G3-T89 | Subscription DELETE exists over ARM | `DELETE subscriptions/{s}` => 200, missing => 204; under a missing topic => 404 (code `(verify)`); `terraform destroy` of `azurerm_servicebus_subscription` succeeds | T2/T3 | Spec: DELETE 200/204. The destroy path is the reason this id is high value |
| G3-T90 | Same-named subscriptions under two topics are independent | create `s1` under `t1` and `t2`; list per topic returns only its own; delete under `t1` leaves `t2/s1` | T2 | Key-scheme check: the key must include the topic. Can also run as an `internal/arm` store unit test |

### Rules

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T91 | A new subscription gets a `$Default` rule | after `PUT` subscription, `GET rules` => `$Default`; after adding `r1`, list => `$Default` and `r1` | T2 | Real ARM body is probably `filterType` `SqlFilter` with `sqlExpression` `1=1` `(verify)`. Engine-gap: auto-child |
| G3-T92 | Rule filters persist | `PUT rules/{r}` with `sqlFilter.sqlExpression` => `GET` returns it with `compatibilityLevel` 20 `(verify)` and `requiresPreprocessing`; a `correlationFilter` with `correlationId`, `messageId`, `to`, `replyTo`, `label`, `sessionId`, `replyToSessionId`, `contentType`, `properties` round-trips; `action.sqlExpression` round-trips | T2 | The ARM surface uses `label`, not `subject`. `filterType` is `SqlFilter` or `CorrelationFilter` |
| G3-T93 | Rule delete and missing parents | `DELETE rules/{r}` => 200, then `GET` => 404; `PUT` under a missing subscription => 404 | T2 | Spec: DELETE 200/204 |

### Network rule set

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T94 | `networkRuleSets/default` is readable on a fresh namespace | `GET namespaces/{n}/networkRuleSets/default` => 200, `defaultAction` `Allow`, `publicNetworkAccess`, empty `virtualNetworkRules` and `ipRules`; `PUT` round-trips; no DELETE | T2/T3 | Real Azure may restrict rule sets to Premium `(verify)`; serving the default for every sku is the safe superset. Engine-gap: singleton |

### New

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-SB-N1 `new` | First PUT of a queue, topic, subscription, rule or authorization rule is accepted by azurerm and azapi | `terraform apply` creates each child; no "unexpected status" error | T3 | The spec lists only 200 for these PUTs; the engine answers 201 on create (ADR 0006). If azurerm rejects 201 here, a per-type create status is needed `(verify)` |

### Data plane (bespoke)

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T95 | A message whose delivery count exceeds `maxDeliveryCount`, or that expires with `deadLetteringOnMessageExpiration`, lands in the dead-letter queue | repeated receive without complete => message readable from the DLQ path; expired message likewise | T2 | Bespoke. Port the semantics on the JSON data plane; it has no DLQ today. Transport is JSON, not AMQP |
| G3-T96 | Dead-lettered messages are forwarded to `forwardDeadLetterMessagesTo` | dead-letter a message on `q1` with a forward target `q2` => it arrives on `q2` | T2 | Bespoke. Depends on queue forwarding properties and dead-lettering |
| G3-T97 | Topic publish is filtered per subscription by its rules | publish to `t` with two subscriptions with different filters => each receives only matching messages | T2 | Bespoke. Needs the ARM subscriptions and rule filters. Data plane has no subscriptions today |

### Terraform

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T99 | Namespace, queue and topic apply, read back and destroy | `azurerm_servicebus_namespace`, `_queue`, `_topic` and azapi `Microsoft.ServiceBus/namespaces`, `queues`, `topics` all apply; second `plan` is empty; `destroy` succeeds | T3 | New cases `azurerm/servicebus` and `azapi/servicebus`. Add `azurerm_servicebus_subscription` and `_rule` once subscription delete works |

## Pitfalls

- Real Azure has an ARM DELETE for subscriptions (200/204). Omitting it breaks `terraform destroy`.
- On the ARM surface a correlation filter uses `label`, not `subject`; timestamps are `createdAt` and `updatedAt`.
- Entity not-found is the generic `ResourceNotFound`, not a per-entity code `(verify)`.
- Child PUTs are 200 in the spec, not 201; see the new-case row.
- Namespace names are global, and a deleted namespace must release its name and every child (ARM and data-plane keys).
- `networkRuleSets/default` must be GET-able; azurerm reads it during namespace refresh `(verify for non-Premium)`.
- Real namespace DELETE can be asynchronous (202); the engine is sync, so assert 200 or 204.
- Key material appears only in `listKeys` and `regenerateKeys` responses, never in an authorization rule `GET` or list. `listKeys` is a POST.

## Dropped

| ID | Reason |
|---|---|
| G3-T98 | Sessions (`requiresSession`) need AMQP; not portable to the JSON data plane and low value |
