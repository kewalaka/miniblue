# Event Hubs

> Engine fit: hooks for `namespaces`, passthrough child for `eventhubs` and `authorizationRules` (with a POST action for the keys), passthrough singleton for `networkRuleSets/default`, bespoke for the AMQP and Kafka data planes (out of scope). miniblue today: none (no `internal/services` package, no registration for `Microsoft.EventHub`). Suggested wave: new types after G2 ([SPIKE](../SPIKE.md) rank 5).

**The data plane is out of scope.** No AMQP listener, no Kafka endpoint, no message storage. The `serviceBusEndpoint` string is derived and does not resolve. The control plane is enough for azurerm, azapi and Bicep to plan, apply and destroy.

## Types

Latest stable api-version is 2026-01-01 (confirmed in the REST spec repo). Other stable versions: 2014-09-01, 2015-08-01, 2017-04-01, 2021-11-01, 2024-01-01; previews run to 2026-07-01-preview. azurerm imports the 2024-01-01 clients for namespaces, network rule sets and namespace authorization rules (provider source, verify per release). `APIVersions` is metadata only (ADR-0005), so any well-formed version is accepted.

Name rules from Learn:

- Namespace: 6 to 50 characters, alphanumerics and hyphens, starts with a letter, ends with a letter or number, globally unique.
- Eventhub: 1 to 256 characters of alphanumerics, periods, hyphens and underscores, starting and ending alphanumeric.
- Authorization rule: 1 to 50 characters, same character set, starting and ending alphanumeric.

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.EventHub/namespaces` | 2026-01-01, 2024-01-01 | RG | eventhubs, authorizationRules, networkRuleSets | list by subscription and by RG; `POST /subscriptions/{s}/providers/Microsoft.EventHub/checkNameAvailability` (no source scenario) | hooks | `Hooks.Defaults` (`provisioningState`, `status`, `createdAt`, `updatedAt`, `serviceBusEndpoint`, `metricId`); `PatchMode` merge (real `PATCH` exists); a create-time hook that seeds the default authorization rule; `Reader.Under` for the subscription list; global name uniqueness | H | S |
| `namespaces/eventhubs` | 2026-01-01, 2024-01-01 | child of namespaces | authorizationRules, consumergroups (no source scenario) | list by namespace | passthrough | parent existence (built into the engine, ADR-0003); `partitionCount` 1 to 32, `messageRetentionInDays` 1 to 7 per the spec description (verify) | H | S |
| `namespaces/authorizationRules` | 2026-01-01, 2024-01-01 | child of namespaces | none | `POST listKeys`; `POST regenerateKeys` body `{keyType, key?}` | passthrough | `Type.Actions`; key generation; seed `RootManageSharedAccessKey` on namespace create (verify) | M | S |
| `namespaces/eventhubs/authorizationRules` | 2026-01-01, 2024-01-01 | child of eventhubs | none | `POST listKeys`; `POST regenerateKeys` | passthrough | same as namespace rules | L | S |
| `namespaces/networkRuleSets/default` | 2026-01-01, 2024-01-01 | child of namespaces; singleton | none | `GET .../networkRuleSets` list | passthrough | `Hooks.Validate` (name must be `default`); a GET that succeeds before any PUT | L | S |
| AMQP and Kafka data plane | not ARM | per namespace endpoint | none | produce and consume | bespoke, out of scope | an AMQP 1.0 listener; message store | M | L |

Spec status codes: namespace `PUT` is 200, 201 or 202 (LRO); eventhub `PUT` is 200 only; rule `PUT` is 200; `DELETE` is 200 or 204 (namespace also 202); `listKeys` and `regenerateKeys` are 200. miniblue is sync (ADR-0006): `PUT` 201 on create and 200 on replace, `DELETE` 200 or 204.

## miniblue gaps

- No Event Hubs support at all. `Microsoft.EventHub` is not registered and no handler exists, so any `azurerm_eventhub*` or Bicep deployment fails on the first namespace `PUT`.
- azurerm's namespace read calls `GET networkRuleSets/default` and `listKeys` on the default `RootManageSharedAccessKey` rule (provider source, namespace resource read path). A namespace that does not seed both makes the read fail even when `PUT` succeeds (verify the exact failure).
- Global namespace-name uniqueness has no precedent in the engine. Decide the behaviour (see Pitfalls).

## Scenarios

`{p}` is `/subscriptions/{s}/resourceGroups/{rg}/providers/Microsoft.EventHub`. Statuses follow ADR-0006 (sync).

### namespaces

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T54 | Terraform azurerm creates and destroys a namespace. | `azurerm_eventhub_namespace` apply then destroy => both succeed with no drift on a second plan | T3 | Apply path: namespace `PUT`, read (`GET` namespace, `GET networkRuleSets/default`, `listKeys` on `RootManageSharedAccessKey`), destroy `DELETE` then a poll until `GET` => 404. Needs `sku` and defaults. Pins 2024-01-01 (verify per release). |
| G2-T55 | Terraform azapi creates and destroys a namespace. | `azapi_resource` of type `Microsoft.EventHub/namespaces@<version>` apply then destroy => both succeed | T3 | azapi sends the body as written and reads back the `PUT` response. Works at any well-formed api-version because `APIVersions` is metadata only. |
| G2-T58 | A namespace can be created, read and updated. | `PUT {p}/namespaces/{n}` body `{location, sku{name, capacity}}` => 201; `GET` => 200; `PUT` again or `PATCH` with changed properties => 200 and the new values on `GET` | T2 | Properties to round-trip: `tags`, `sku`, `isAutoInflateEnabled`, `maximumThroughputUnits`, `kafkaEnabled`, `minimumTlsVersion`, `publicNetworkAccess`. `sku.name` is required (Basic, Standard or Premium); capacity range per the spec description (0 to 20 for Basic and Standard, 0 to 10 for Premium; verify). `PATCH` exists in the real API and runs in merge mode. |

### eventhubs

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T56 | Terraform creates and destroys an event hub with both providers. | `azurerm_eventhub` and an `azapi_resource` of `Microsoft.EventHub/namespaces/eventhubs` apply then destroy => both succeed | T3 | Needs `partitionCount` and `messageRetentionInDays` (spec 1 to 7, verify) round-tripped, and the server-set `partitionIds`, `status`, `createdAt`, `updatedAt`. Flag: the spec lists only 200 for the eventhub `PUT`; the engine returns 201 on create (ADR-0006). Clients accept both (verify with the pinned azurerm version). |
| G2-T57 | `az eventhubs eventhub` works end to end. | `create` => the hub exists; `show` => 200; `list` => contains every hub in the namespace; `delete` => gone, `list` no longer has it | T2/T4 | The REST equivalent is `PUT`, `GET`, `GET {p}/namespaces/{n}/eventhubs` and `DELETE` => 200 then 204 on repeat. No data-plane call is involved. |
| G2-T62 | Eventhub operations under a namespace that does not exist fail with a not-found. | `PUT {p}/namespaces/{n}/eventhubs/{h}` with no namespace => 404 `ParentResourceNotFound`; `GET` of a missing hub in an existing namespace => 404 `ResourceNotFound` | T1/T2 | Asserted codes are the generic ARM ones from the engine ([ADR-0003](../adr/0003-pipeline-phases-and-hooks.md)). Real Event Hubs may answer `ResourceNotFound` for a missing namespace (verify); azurerm treats any 404 as absent, so assert 404 and accept either code if real-Azure parity matters. Flag: per-service not-found codes are not asserted. |

### networkRuleSets and authorizationRules

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T59 | `networkRuleSets` exists only as `default`. | `PUT {p}/namespaces/{n}/networkRuleSets/default` => 200; `GET` returns `trustedServiceAccessEnabled`, `defaultAction` (Allow or Deny), `virtualNetworkRules`, `ipRules`, `publicNetworkAccess` (Enabled, Disabled or SecuredByPerimeter); any other name => 404 | T2 | In the spec the path is the literal `default`, so other names have no route; the exact error code is (verify). `GET` before any `PUT` should return a default set (`Allow`, empty rules; verify) because azurerm reads it unconditionally. `GET .../networkRuleSets` lists the single `default`. |
| G2-T60 | `listKeys` on an authorization rule returns the keys and connection strings. | `POST {p}/namespaces/{n}/authorizationRules/{r}/listKeys` => 200 `{primaryConnectionString, secondaryConnectionString, aliasPrimaryConnectionString, aliasSecondaryConnectionString, primaryKey, secondaryKey, keyName}`; `keyName` equals `{r}`; keys are base64 256-bit values; missing rule or namespace => 404 | T2 | Shape confirmed against the spec; the connection-string format `Endpoint=sb://{n}.servicebus.windows.net/;SharedAccessKeyName={r};SharedAccessKey={key}` is (verify). Alias fields are empty without geo-disaster recovery (verify). The rule `PUT` requires `properties.rights` (Manage, Send, Listen); `Manage` alone being invalid is (verify). `regenerateKeys` takes `{keyType: PrimaryKey or SecondaryKey, key?}` (`keyType` required) and changes only that key. Namespace create seeds `RootManageSharedAccessKey` with all three rights (verify); azurerm depends on it. |

## Pitfalls

- Namespace names are DNS labels and unique across Azure. A duplicate name in another resource group or subscription conflicts in real Azure. Decide whether miniblue enforces uniqueness (cheap to do with `Reader.Under` in a single-tenant store) and note it in the PR.
- The eventhub `PUT` is 200 in the spec, not 201. A test that asserts 201 on real Azure fails; accept 200 or 201.
- Namespace `PUT` is an LRO (200, 201 or 202) in real Azure. miniblue is sync (ADR-0006); SDK pollers accept the synchronous result.
- The key-rotation action is `regenerateKeys` with `keyType`, unlike Event Grid's `regenerateKey` with `keyName`. `listKeys` returns six string fields plus `keyName`, not just two keys.
- Keys are 256-bit base64 values. Generate them randomly at rule create, keep them stable until regenerated, and keep primary and secondary distinct.
- `RootManageSharedAccessKey` exists on every real namespace. Without it, azurerm's read fails or drifts (verify).
- `networkRuleSets/default` is a singleton that exists before any `PUT`. A 404 on first `GET` breaks azurerm's read. `publicNetworkAccess` appears on both the namespace and the rule set.
- Invented service-specific not-found codes are wrong; ARM clients key off `ResourceNotFound` and `ParentResourceNotFound`.
- Partition count and retention are constrained: `partitionCount` is 1 to 32 and cannot change after create on Basic and Standard (verify); retention is 1 to 7 days per the spec description (verify).
- Ids are case-insensitive. Normalise on lookup and return canonical casing.

## Dropped

| ID | Reason |
|---|---|
| G2-T61 | AMQP producer and consumer round trip: data plane, out of scope (no AMQP listener). |
