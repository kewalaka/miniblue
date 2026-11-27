# Service Bus

> Engine fit is hooks for namespaces, passthrough children for authorizationRules, queues, topics, subscriptions and rules, engine gaps for the `$Default` rule and `networkRuleSets/default`, and bespoke for the messaging data plane. miniblue today has a hand-written service `internal/services/servicebus` covering ARM namespaces, queues, topics, and a JSON data plane. Suggested wave is PLAN wave 4 for namespaces, queues, and topics, with subscriptions, rules and authorizationRules as new types after G2. The data plane never migrates.

Paths below are relative to `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.ServiceBus`. Spec facts come from `Microsoft.ServiceBus/ServiceBus` in the Azure REST API specs (latest stable `2026-01-01`, latest preview `2026-07-01-preview`).

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `namespaces` | real latest stable `2026-01-01`; azurerm pin `(verify)` | RG; list by RG and subscription | authorizationRules, queues, topics, `networkRuleSets/default` | PATCH; `checkNameAvailability` POST (chi route) | hooks | `Hooks.Defaults` (endpoint, metricId, timestamps, `RootManageSharedAccessKey`), `PatchMerge`, subscription-wide list (`Under`), global-unique name has no seam | H | M |
| `namespaces\|queues\|topics/authorizationRules` | same (spec path uses `AuthorizationRules`; ARM paths are case-insensitive) | namespace, queue, topic | none | `listKeys`, `regenerateKeys` (POST, 200) | passthrough child + actions | `Type.Actions` (lands with the first `listKeys` consumer), key generation in an action | H | M |
| `namespaces/queues` | same | namespace | authorizationRules | none | passthrough + `Defaults` | `Hooks.Defaults` for the property defaults | M | S |
| `namespaces/topics` | same | namespace | subscriptions, authorizationRules | none | passthrough + `Defaults` | `Hooks.Defaults`, `DeleteTree` for the cascade | M | S |
| `topics/subscriptions` | same | topic | rules | none (PUT, GET, DELETE, list) | passthrough child + auto-child | child type; `$Default` rule seed has no seam | H | S to M |
| `subscriptions/rules` | same | subscription | none | none | passthrough child | child type | H | S |
| `namespaces/networkRuleSets/default` | same | namespace | none | GET, PUT only; no DELETE | engine gap (singleton) | no seam; use a bespoke GET with defaults | H (azurerm reads it) | S |
| Data plane (queue and topic messaging, dead-letter, forwarding, sessions) | not applicable | namespace endpoint | none | send, receive, publish | bespoke | none; stays chi | M | L |

## miniblue gaps

Ordered by value.

- `subscriptions`, `rules`, `authorizationRules`, `listKeys` and `regenerateKeys` are missing. `azurerm_servicebus_subscription` and `azurerm_servicebus_namespace` (connection strings) cannot work.
- There is no ARM DELETE for subscriptions, which `azurerm_servicebus_subscription` destroy needs. Real Azure supports it (200 or 204 in the spec).
- `networkRuleSets/default`, list by subscription, and PATCH are missing.
- Namespace responses are fixed to location `eastus` and hard-coded `createdAt` and `updatedAt` (`handler.go:220`, `handler.go:230-231`). Queue and topic ARM PUT also build a fixed body (`handler.go:311-325`), so request body properties and tags are dropped.
- Namespace DELETE removes only data-plane keys (`sb:queue:`, `sb:topic:`) (`handler.go:277-278`). ARM queues and topics (`sb:armqueue:`, `sb:armtopic:`; `handler.go:292`, `handler.go:367`) survive and reappear if the namespace is recreated.
- Namespace DELETE of an existing namespace returns 202 with no `Location` (`handler.go:281`), and deleting a missing one returns 404 (real Azure returns 204).
- ARM queues and topics are a separate key space from the data-plane queues and topics, so an entity created over ARM is not the same object the data plane sends to.
- The resource group cascade covers `sb:namespace:`, `sb:armqueue:`, and `sb:armtopic:` (`resourcegroups/handler.go:192-194`) but not data-plane keys or any new type.

## Scenarios

### Namespaces

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T78 | Namespace body carries service-generated fields, and an empty list is `{"value":[]}`. | `PUT` and `GET namespaces/{n}` return `serviceBusEndpoint`, `metricId`, `disableLocalAuth`, `minimumTlsVersion`, `publicNetworkAccess`, `provisioningState` `Succeeded`, `createdAt`, and `updatedAt`; `GET` list with none returns `{"value":[]}`. | T2 | Spec field names are `createdAt` and `updatedAt` (not `createdOn` and `updatedOn`). `status` is also read-only in the spec. |
| G3-T79 | List by subscription spans resource groups. | `GET /subscriptions/{sub}/providers/Microsoft.ServiceBus/namespaces` returns 200 and includes namespaces from two resource groups. | T1/T2 | Needs engine `Under` subscription-wide list. Missing in miniblue. |
| G3-T80 | Deleting a namespace removes every child and frees the name. | `DELETE namespaces/{n}` returns 200; `GET` of a queue, topic, subscription, rule, or authorization rule returns 404; recreating the namespace shows no children; `DELETE` on a missing namespace returns 204. | T2 | Spec lists 200, 202 or 204. miniblue orphans ARM queues and topics today. |
| G3-T81 | Namespace names are globally unique. | A second `PUT` of the same name in another resource group or subscription returns 409 (code `(verify)`); after `DELETE`, `PUT` returns 201. | T2 | Engine gap for global uniqueness. |
| G3-T82 | PUT in a missing resource group fails, and `sku` is required. | `PUT` in an unknown resource group returns 404 `ResourceGroupNotFound`; `PUT` without `sku` returns 400 (code `(verify)`). | T1/T2 | Spec requires `sku.name`; whether `sku` itself is required on the namespace is `(verify)`. |

### Authorization rules

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T83 | A new namespace has `RootManageSharedAccessKey`, and `listKeys` returns keys and connection strings. | After `PUT` namespace, `GET authorizationRules/RootManageSharedAccessKey` returns `rights` Listen, Manage, and Send; `POST .../listKeys` returns 200 with `keyName`, `primaryKey`, `secondaryKey`, `primaryConnectionString` (containing `SharedAccessKey=`), `secondaryConnectionString`, `aliasPrimaryConnectionString`, and `aliasSecondaryConnectionString`. | T2/T3 | Spec marks all `listKeys` fields read-only. Alias fields are empty without geo-DR `(verify)`. Needs `Type.Actions` and an auto-child on namespace create. |
| G3-T84 | Authorization rules work on namespace, queue and topic, and `regenerateKeys` rotates one key. | `PUT` with `rights` returns 200; `GET`, `GET` list, and `DELETE` (200, missing 204) succeed; `POST regenerateKeys` with `{"keyType":"PrimaryKey"}` returns 200 and changes only `primaryKey`; `SecondaryKey` changes only `secondaryKey`. | T2 | Body also accepts an optional `key` to set the value. The `rights` enum is Listen, Send, and Manage. |

### Queues

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T85 | Queue defaults and update. | `PUT queues/{q}` returns 200 or 201 with `lockDuration` `PT1M`, `maxSizeInMegabytes` 1024, `maxDeliveryCount` 10, `duplicateDetectionHistoryTimeWindow` `PT10M`, `requiresSession` false, `deadLetteringOnMessageExpiration` false, and `defaultMessageTimeToLive` and `autoDeleteOnIdle` at the maximum duration; a second `PUT` with changes is reflected on `GET`. | T2 | Defaults and the maximum-duration string are `(verify)`. Spec lists 200 for PUT; see the new-case row. Uses `Defaults` hook. |
| G3-T86 | Queue forwarding properties round-trip, delete, and list. | `PUT` with `forwardTo` and `forwardDeadLetterMessagesTo` returns them on `GET`; `DELETE` returns 200, or 204 when missing; `GET` list works including empty `{"value":[]}`. | T2 | ARM part only. The messaging behaviour is in the data-plane forwarding row. |

### Topics

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T87 | Topic CRUD, update, list, and delete cascading subscriptions. | `PUT`, `GET`, and `DELETE topics/{t}` succeed; updates are reflected on `GET`; `GET` list works including empty; `DELETE` topic then `GET` its subscriptions and rules returns 404. | T2 | Needs `DeleteTree` for the cascade. |

### Subscriptions

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T88 | Subscription create is idempotent with defaults, and missing returns 404. | `PUT topics/{t}/subscriptions/{s}` returns 200 or 201 (calling it twice is not an error); `GET` returns `maxDeliveryCount` 10 and `deadLetteringOnFilterEvaluationExceptions` true; `GET` missing returns 404 `ResourceNotFound`. | T2 | Missing in miniblue. Not-found code is the generic one, not a per-entity code `(verify)`. |
| G3-T89 | Subscription DELETE exists over ARM. | `DELETE subscriptions/{s}` returns 200, or 204 when missing; under a missing topic it returns 404 (code `(verify)`); `terraform destroy` of `azurerm_servicebus_subscription` succeeds. | T2/T3 | Spec lists DELETE 200 or 204. The destroy path is the reason this id is high value. |
| G3-T90 | Same-named subscriptions under two topics are independent. | Creating `s1` under `t1` and `t2` lists each only under its own topic, and deleting under `t1` leaves `t2/s1`. | T2 | The key must include the topic. Can also run as an `internal/arm` store unit test. |

### Rules

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T91 | A new subscription gets a `$Default` rule. | After `PUT` subscription, `GET rules` returns `$Default`; after adding `r1`, list returns `$Default` and `r1`. | T2 | Real ARM body is `filterType` `SqlFilter` with `sqlExpression` `1=1` `(verify)`. Engine gap for auto-child. |
| G3-T92 | Rule filters persist. | `PUT rules/{r}` with `sqlFilter.sqlExpression` returns it on `GET` with `compatibilityLevel` 20 `(verify)` and `requiresPreprocessing`; a `correlationFilter` with `correlationId`, `messageId`, `to`, `replyTo`, `label`, `sessionId`, `replyToSessionId`, `contentType`, and `properties` round-trips; `action.sqlExpression` round-trips. | T2 | The ARM API uses `label`, not `subject`. `filterType` is `SqlFilter` or `CorrelationFilter`. |
| G3-T93 | Rule delete and missing parents. | `DELETE rules/{r}` returns 200 and `GET` after returns 404; `PUT` under a missing subscription returns 404. | T2 | Spec lists DELETE 200 or 204. |

### Network rule set

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T94 | `networkRuleSets/default` is readable on a fresh namespace. | `GET namespaces/{n}/networkRuleSets/default` returns 200 with `defaultAction` `Allow`, `publicNetworkAccess`, and empty `virtualNetworkRules` and `ipRules`; `PUT` round-trips; no DELETE exists. | T2/T3 | Real Azure may restrict rule sets to Premium `(verify)`; serving the default for every sku is the safe superset. Engine gap for singleton. |

### New

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-SB-N1 `new` | First PUT of a queue, topic, subscription, rule or authorization rule is accepted by azurerm and azapi. | `terraform apply` creates each child with no "unexpected status" error. | T3 | The spec lists only 200 for these PUTs, while the engine answers 201 on create (ADR-0006). If azurerm rejects 201 here, a per-type create status is needed `(verify)`. |

### Data plane (bespoke)

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T95 | A message whose delivery count exceeds `maxDeliveryCount`, or that expires with `deadLetteringOnMessageExpiration`, lands in the dead-letter queue. | Repeated receive without complete makes the message readable from the DLQ path, and an expired message does the same. | T2 | Bespoke. Port the semantics on the JSON data plane, which has no DLQ today. Transport is JSON, not AMQP. |
| G3-T96 | Dead-lettered messages are forwarded to `forwardDeadLetterMessagesTo`. | Dead-lettering a message on `q1` with forward target `q2` delivers it on `q2`. | T2 | Bespoke. Depends on queue forwarding properties and dead-lettering. |
| G3-T97 | Topic publish is filtered per subscription by its rules. | Publishing to `t` with two subscriptions with different filters delivers only matching messages to each. | T2 | Bespoke. Needs the ARM subscriptions and rule filters. Data plane has no subscriptions today. |

### Terraform

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T99 | Namespace, queue and topic apply, read back and destroy. | `azurerm_servicebus_namespace`, `_queue`, `_topic` and azapi `Microsoft.ServiceBus/namespaces`, `queues`, `topics` all apply; a second `plan` is empty; `destroy` succeeds. | T3 | New cases `azurerm/servicebus` and `azapi/servicebus`. Add `azurerm_servicebus_subscription` and `_rule` once subscription delete works. |

## Pitfalls

- Real Azure has an ARM DELETE for subscriptions (200 or 204). Omitting it breaks `terraform destroy`.
- In the ARM API a correlation filter uses `label`, not `subject`, and timestamps are `createdAt` and `updatedAt`.
- Entity not-found is the generic `ResourceNotFound`, not a per-entity code `(verify)`.
- Child PUTs return 200 in the spec, not 201; see the new-case row.
- Namespace names are global, and a deleted namespace must release its name and every child (ARM and data-plane keys).
- `networkRuleSets/default` must be readable with GET, because azurerm reads it during namespace refresh `(verify for non-Premium)`.
- Real namespace DELETE may be asynchronous (202); the engine is sync, so assert 200 or 204.
- Key material appears only in `listKeys` and `regenerateKeys` responses, never in an authorization rule `GET` or list. `listKeys` is a POST.

## Dropped

| ID | Reason |
|---|---|
| G3-T98 | Sessions (`requiresSession`) need AMQP, which is not portable to the JSON data plane and has low value. |
