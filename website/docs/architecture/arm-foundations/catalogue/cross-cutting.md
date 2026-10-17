# Cross-cutting scenarios

> Engine fit: the first block is the generic engine contract (armtest rows, per type). Resource groups, providers, subscriptions and deployments are bespoke handlers today and stay out of the engine until PLAN 3.x. miniblue today: the engine serves one type (`policyDefinitions`); everything else is hand-written under `internal/services/*`. Suggested wave: engine contract with PLAN 1.5, the rest as the PLAN row named in each Notes cell.

This page holds the scenarios that are not owned by one service: behaviour every ARM type shares, plus resource groups, subscriptions, providers, deployments and client smoke. Per-service scenarios live on the service pages in this catalogue. Ids keep the numbering of the spike so ADR and PLAN references resolve.

Codes and statuses not marked `(verify)` come from the ARM contract as understood during the spike and from ADR-0003, ADR-0005 and ADR-0006. None was re-checked against the live service. Treat every code as provisional until a conformance test pins it.

## How to read the tables

| Column | Meaning |
|---|---|
| Behaviour | Real Azure behaviour, not what miniblue does today |
| Expect | `verb path => status, key body facts, error code` |
| Tier | T1 conformance (armtest row, generic), T2 per-resource httptest in `tests/`, T3 Terraform acceptance, T4 client smoke (SDK, `az`) |
| Notes | armtest check name (ADR-0005), PLAN row, miniblue status. `file:line` is cited only where checked in the worktree |

Related: [ADR-0003 pipeline](../adr/0003-pipeline-phases-and-hooks.md), [ADR-0005 conformance suite](../adr/0005-type-metadata-and-conformance.md), [ADR-0006 sync and errors](../adr/0006-sync-lro-and-errors.md), [PLAN](../PLAN.md).

## armtest additions beyond ADR-0005

ADR-0005 defines `Sample`, `Run` and `Coverage` and the check list. Nothing here replaces it. The scenarios below need these extra checks, which derive from the same `Type` metadata and need no Options struct. Propose them as an ADR-0005 amendment or as extra checks in PLAN 1.5.

| Proposed check | Asserts | Derives from |
|---|---|---|
| WrongMethod | `POST` or `PUT` on a collection, and an unsupported verb on an item, give 405 `MethodNotAllowed` with the envelope | `Type.Patch` (`PatchNone` also gives 405 on PATCH) |
| MalformedAPIVersion | `api-version=latest` gives 400 `InvalidApiVersionParameter`; a well-formed unknown date is accepted | none (ADR-0005 states the rule but lists only the missing case) |
| ScopeSurvivesRGDelete | A type whose only scope is subscription survives deletion of an RG | `Type.Scopes` |
| ResponseHeaders | `x-ms-request-id` and `x-ms-correlation-request-id` on success and on error (ErrorEnvelope asserts the request id on 4xx only) | none |
| UnknownRoute | An unregistered type or an unrouted path gives a JSON 404 envelope with an ARM code | ADR-0002 (code is open, see the unknown route row) |

Two ADR-0005 checks need a precise reading. UrlCasing says response casing is preserved: read it as canonical keywords plus the name casing of the creating PUT (ADR-0003). List asserts `{"value":[...]}` only, because paging is not planned.

## Scenarios

### Engine contract (armtest)

Rules from ADR-0003 and ADR-0006 that apply to every row: api-version is always required, parent and RG existence is always checked, responses are canonical-cased, writes are synchronous.

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| X1 | PUT a new resource at each scope the type allows | `PUT {id}` => 201; `id`, `name`, `type` are server-owned; submitted properties echoed | T1 | armtest PutCreate |
| X2 | PUT an existing resource with a different body | `PUT {id}` => 200; full replace, properties missing from the body are gone | T1 | armtest PutReplace. Server-owned fields (keys, creation time) must survive via `Request.Current`; the check cannot see them, so types that own such fields add a T2 |
| X3 | GET an existing and a missing resource | `GET {id}` => 200, same document as the PUT; missing => 404 `ResourceNotFound` with the envelope | T1 | armtest Get, GetMissing |
| X4 | GET with a mangled-case path | `GET {MANGLED id}` => 200; `id` keeps the canonical segment keywords and the name casing of the creating PUT | T1 | armtest UrlCasing. Engine types are canonical-only (ADR-0003). Legacy handlers still mirror the client casing (`tests/casesensitivity_test.go`) until migrated. The azapi policy definition acceptance already covers it end to end |
| X5 | List a collection: empty, N items, siblings only | `GET {collection}` => 200 `{"value":[...]}` in a stable order; no leak across parents or across name prefixes; `nextLink` absent | T1 | armtest List, ListSubscription, PrefixSibling. Paging is deferred (no PLAN row; see engine lessons). Real RPs add `nextLink` only when paging; `(verify)` per RP |
| X6 | DELETE an existing, then a missing resource | `DELETE {id}` => 200 when it existed, then 204 with an empty body; `GET` => 404 | T1 | armtest Delete, DeleteMissing. Real RPs vary between 200, 202 and 204; sync-only (ADR-0006) fixes 200 and 204 |
| X7 | Unsupported or malformed api-version | `api-version=latest` => 400 `InvalidApiVersionParameter` | T1 | Real ARM also rejects a well-formed version the type does not support `(verify)`; miniblue deliberately accepts any well-formed version and treats `APIVersions` as metadata (ADR-0005). Needs the MalformedAPIVersion check above |
| X8 | Missing api-version | any `/subscriptions/...` call without `api-version` => 400 `MissingApiVersionParameter` | T1 | armtest MissingAPIVersion. Always strict (PLAN 1.4). Legacy routes only echo `x-ms-api-version` today (`internal/server/middleware.go:46`), so they still accept a missing version |
| X9 | Body `name` or `id` differs from the URL | `PUT` => 400 `InvalidRequestContent` `(verify)` | T1 | armtest NameIDMismatch |
| X10 | Malformed JSON, non-object body, empty body | `PUT` => 400 JSON envelope `InvalidRequestContent`; never 500, never `text/plain` | T1 | armtest BadBody |
| X11 | PATCH per `PatchMode` | `PatchMerge`: 200, RFC 7386 merge (`null` deletes); `PatchTags`: 200, tags replaced, `{}` clears, no `tags` => 400; `PatchNone`: 405; missing resource => 404 | T1/T2 | armtest Patch. Mode comes from `Type.Patch` (PLAN 1.3). Types with PATCH-specific hooks add a T2 |
| X12 | Delete a parent | children `GET` => 404; child list empty; recreating the parent starts empty | T1 | armtest ChildCascade |
| X13 | Delete an RG that holds an engine type at RG scope | `DELETE rg` => 200/202; resource `GET` => 404; RG-wide and subscription-wide lists empty | T1/T2 | armtest RGCascade. miniblue defect today: RG delete walks a hard-coded legacy prefix list. Fixed by scope cleaners (PLAN 0.6) and `Engine.DeleteScope` (PLAN 1.2) |
| X14 | Delete an RG while a subscription-scope type has resources | resource `GET` => 200; subscription-wide list unchanged | T2 | Gap in ADR-0005: no check. Propose ScopeSurvivesRGDelete. Guards over-eager cascade when `DeleteScope` lands |
| X15 | Unsupported verb | `POST` or `PUT` on a collection, `POST` on an item without a registered action => 405 `MethodNotAllowed` | T1 | Gap in ADR-0005. Propose WrongMethod. Real ARM also sends an `Allow` header `(verify)` |
| X16 | Every non-2xx response | JSON `{"error":{"code","message"}}`, `application/json`; `target` and `details` where validation names a field | T1/T2 | armtest ErrorEnvelope. `target`/`details` arrive with `azerr.Error` (PLAN 0.7) |
| X17 | Correlation headers | every response carries `x-ms-request-id` and `x-ms-correlation-request-id` | T2 | Gap in ADR-0005 (ErrorEnvelope checks the request id on 4xx only). Both headers are set by `AzureHeaders` (`internal/server/middleware.go:32-33`) |
| X18 | Unknown route or unregistered type | `GET /subscriptions/{s}/providers/Microsoft.Nope/things?api-version=...` => 404 JSON envelope with an ARM code, not `EndpointNotFound` | T2 | ADR-0002 fixes a strict 404. The code is `InvalidResourceType` or `NoRegisteredProviderFound` `(verify)`; pick one in the PLAN row that implements ADR-0002 and pin it here |
| X19 | Parallel PUT, PATCH, DELETE on one id | no 5xx, no torn documents, under `-race` | T2 | Stays one test in `internal/arm` (ADR-0005). Only meaningful once CI runs the race detector (PLAN 0.1) |
| X20 | Tags lifecycle | create with tags, read back via GET and list, update, clear with `{}`; tags preserved verbatim | T3 | azurerm fixture with tags on RG and one engine type. Per-service legacy coverage is in `tests/tags_test.go` |
| X21 | provisioningState on synchronous types | `PUT` and `GET` => `properties.provisioningState == "Succeeded"` | T1 | armtest ProvisioningState, gated by `Type.Provisioned`. Real LRO types may show `Creating`, `Updating`, `Deleting` first; not modelled (ADR-0006) |

### Resource groups

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| X23 | RG delete is long-running | `DELETE rg` => 202 with `Location` pointing at `operationresults`; polling => 200 | T2 | Pins today's legacy behaviour (`internal/services/resourcegroups/handler.go:221-223` serves `operationresults`). PLAN 3.3 makes RG delete sync 200 and drops `operationresults` (ADR-0006); update this row in that PR. Deliberate deviation from real ARM |
| X24 | PUT an existing RG with a different location | `PUT rg` => 409 `InvalidResourceGroupLocation` | T2 | miniblue accepts and overwrites the location today (`handler.go:71-91` never compares) |
| X25 | PUT an RG without `location`, or with an invalid one | missing => 400; invalid => 400 `LocationNotAvailableForResourceGroup` `(verify)` | T2 | Missing location is rejected today with a generic 400 (`handler.go:66-69`); the code is not asserted anywhere. Invalid locations are not validated |
| X26 | HEAD an RG | `HEAD rg` => 204 when it exists, 404 when missing, no body | T2 | Exists (route registered in `handler.go`, `r.Head`). Assert both legs |
| X27 | List the resources in an RG | `GET rg/resources` after creating one engine and one legacy resource => both listed with `id`, `name`, `type`, `location`, `tags`; `$filter=resourceType eq '...'` narrows | T2 | Returns `[]` unconditionally today (`handler.go:215-217`). Engine-backed at PLAN 3.3. ADR-0004 lists engine types only and has no `$filter`: gap. azurerm checks this list before deleting an RG (`prevent_deletion_if_contains_resources`, on by default) `(verify)` |
| X28 | DELETE a missing RG | `DELETE rg` => 404 `ResourceGroupNotFound` | T2 | Exists, but returns 404 `ResourceNotFound` with a generic "under resource group" message today (`handler.go:146-149`, via `azerr.NotFound`). Fix the code, and the same for RG GET. Tests that assert the status only miss it |

### Parents, names, locations, ETags

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| X29 | PUT under a missing subscription, RG or parent resource | missing RG => 404 `ResourceGroupNotFound`; missing subscription => 404 `SubscriptionNotFound`; missing parent => 404 `ParentResourceNotFound` (some RPs `ResourceNotFound`) `(verify)` | T1/T3 | armtest ParentMissing. Always on for engine types (ADR-0003). Subscription existence is not checkable until a multi-subscription model exists; `GET /subscriptions/{id}` accepts any id today. T3: a Terraform apply into a missing RG fails with the message surfaced |
| X30 | Invalid resource name | `PUT` => 400 (`InvalidResourceName` or an RP-specific code `(verify)`); `terraform apply` exits non-zero and prints the message | T3 | Negative azurerm fixture. Rules per type in `Validate` through `CheckName` (ADR-0003); ADR-0003 rejects declarative name patterns on `Type`. Backlog item for the fixture is on the client suites page |
| X31 | checkNameAvailability | `POST /subscriptions/{s}/providers/{ns}/checkNameAvailability` `{name,type}` => 200 `{nameAvailable:true}` for an unused name; after create => `false`, `reason: AlreadyExists`; invalid name => `false`, `reason: Invalid` | T2 | Provider-level chi route, not an engine action (ADR-0001). Some RPs use a `locations/{l}` variant `(verify)`. Needs a store lookup by global name; there is no helper yet (see engine lessons) |
| X32 | PUT a regional resource with an unsupported location | `PUT` => 400 `LocationNotAvailableForResourceType` | T2 | Via a `Validate` hook. No shared location helper exists; propose one (engine lessons). Global types have no `location` and are exempt |
| X33 | Optimistic concurrency | `If-Match` mismatch => 412 `PreconditionFailed`; `If-None-Match: *` on an existing resource => 412; a type that requires `If-Match` and gets none => 400 `MissingIfMatchHeader` `(verify)` | T2 | Deferred until a type needs ETags. ADR-0003: the engine ignores `If-Match`, never requires it, and enforces only `If-None-Match: *`. Never require `If-Match` on a path azurerm deletes without it |

### Providers

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| X34 | List providers | `GET /subscriptions/{s}/providers` => 200, 10 or more namespaces each with `namespace`, `registrationState`, `registrationPolicy`; `GET /providers` (tenant) => same shape | T2 | miniblue serves 17 fixed namespaces, all `Registered` (`internal/services/subscriptions/handler.go:94-112`) and has no tenant `/providers` route. PLAN 3.4 derives the list from the registry |
| X35 | Provider state | `GET .../providers/{ns}` => `Registered`; `POST .../unregister` => 200 and later GET/list show `Unregistered`; `POST .../register` => `Registered` | T2 | State is not stored: GET and register echo any namespace as registered (`handler.go:73-81`); no unregister route. No PLAN row stores state (gap) |
| X36 | PUT under an unregistered namespace | `PUT` => 409 `MissingSubscriptionRegistration` | T2/T4 | `az` registers the namespace and retries. azurerm clients run with registrations disabled (`ARM_RESOURCE_PROVIDER_REGISTRATIONS=none`), so a gate would need to default off. No PLAN row (gap) |

### Subscriptions and tenants

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| X37 | Subscription list and get | list => 200; get known => 200 with `state: Enabled`, `subscriptionId`, `tenantId`; get unknown => 404 `SubscriptionNotFound`; malformed id => 400 `InvalidSubscriptionId` `(verify)` | T2 | `GetSubscription` echoes any id as `Enabled` (`handler.go:44-50`), so the unknown and malformed legs fail today. Defining which ids are "known" is a model decision tied to the multi-subscription work in the lifecycle row below |
| X38 | Tenants | `GET /tenants` => 200, `value[].tenantId` | T2 | Exists (`handler.go:52-63`). Assert the shape |
| X39 | Subscription lifecycle and locations | `GET /subscriptions/{s}/locations` => 200, non-empty; subscription create, cancel (`state: Disabled`), enable (`Enabled`), tags set, update and clear | T2 | Deferred: needs a multi-subscription model, which no PLAN row schedules. Real Azure creates subscriptions through aliases and cancels or enables through `Microsoft.Subscription` actions, and manages tags at `.../providers/Microsoft.Resources/tags/default` `(verify)`; assert those shapes, not an invented PUT. The locations leg can land independently |

### Deployments

Bespoke handler. Today it serves RG scope only: list, PUT, GET, DELETE (`internal/services/deployments/handler.go:40-52`), synchronous, with no validate, cancel, export, HEAD, operations or other scopes. Real PUT is asynchronous (accepted, then running, then terminal); the sync deviation returns the terminal state at once (ADR-0006). Sequencing: PLAN 3.1 and 3.2, plus the deployments wave (T5) in [SPIKE](../SPIKE.md).

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| X40 | Create, show, list, delete at RG, subscription, management group and tenant scope | PUT, GET, list, DELETE succeed at each scope; subscription-scope ids contain no `/resourceGroups/` | T2 | Only RG scope exists today. Scope paths: `/subscriptions/{s}/providers/Microsoft.Resources/deployments/{n}`, `/providers/Microsoft.Management/managementGroups/{mg}/providers/Microsoft.Resources/deployments/{n}`, `/providers/Microsoft.Resources/deployments/{n}` |
| X41 | Validate | `POST .../deployments/{n}/validate`: valid template => 200 `properties.provisioningState: Succeeded` (also at tenant scope); structurally invalid template => 400 `InvalidTemplate` `(verify)` | T2 | No validate route today |
| X42 | Cancel | completed deployment => 409 `DeploymentCannotBeCancelled`; nonexistent => 404 at RG, subscription and MG scope; running => `Canceled` | T2 | Under sync-only deployments a running state is unreachable, so the running leg is deferred. 404 code `DeploymentNotFound` `(verify)` |
| X44 | Export | `POST .../exportTemplate` => 200 with the submitted template; nonexistent => 404; `HEAD .../deployments/{n}` => 204 when present (tenant scope too), 404 when absent | T2 | No export or HEAD route today |
| X45 | Outputs | literal, parameter-derived and `tenant().tenantId` outputs resolve; `reference()` on a Key Vault resolves `properties.vaultUri`; `reference()` on an unsupported type fails the deployment | T2 | Only `parameters()` and `variables()` evaluate today. Do not emit a null output for an unresolvable `reference()` |
| X46 | Nested templates | a template with a nested deployment resource creates an RG, the nested deployment and a resource in it; a subscription-scope deployment containing an RG resource creates the RG | T2 | Needs subscription-scope deployments (first row of this group) and in-process dispatch (PLAN 3.1) |
| X47 | Failure | a failing resource gives a terminal `Failed` deployment with `properties.error` (code, message, details); a failing nested deployment is `Failed` too | T2 | miniblue already stores `Failed` plus the error; assert the nested case |
| X48 | Deployment operations | `GET .../deployments/{n}/operations` lists one per resource with `provisioningOperation`, `provisioningState`, `targetResource`; `GET .../operations/{id}` returns one; unknown deployment or operation => 404 (MG and tenant scope too) | T2 | No operations route today. Field names `(verify)` |
| X49 | Deployment of an engine-registered type | the resource appears via the engine `GET` with a canonical id and the same document a direct PUT produces | T2 | In-process `Dispatcher.Put` (PLAN 3.1); legacy types go over the HTTP fallback with the original `Authorization` header. Template resources without `apiVersion` default to `2023-01-01` (`handler.go:188`), so engine types must accept that version |

### Client smoke and hierarchy

Pattern, trust and CI wiring for these rows are on [client suites](client-suites.md).

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| X51 | Azure SDK for Go, `armresources` | RG create, get, list, update tags, exists, delete; policy definition CRUD with the LRO poller; ids canonical-cased; 404 after delete | T4 | New suite `tests/sdk/go` (own module). Pollers must finish against sync responses |
| X52 | Azure SDK for Python, `azure-mgmt-resource` | RG, tags, deployment via `begin_*().result()` | T4 | New pytest module; `base_url`, `credential_scopes`, fake credential |
| X53 | `az` CLI scripted scenario | cloud register, login, `az group create/show/exists/list/delete`, `az policy definition create/show/delete`, `az deployment group create` | T4 | New script plus a CLI version matrix. Feasibility first: the current guide says native `az` is not supported |
| X54 | Terraform negative fixture | apply with an invalid name exits non-zero and the output contains the emulator's message | T3 | New azurerm fixture; pairs with the name validation row. Must assert the message, not only the exit code |
| X55 | Terraform combined fixture | one fixture spanning RG, network, storage, Key Vault, identity; plan after apply shows no drift; second apply is a no-op | T3 | Partly covered by the `examples/terraform` and `network`, `storage`, `keyvault`, `identity-rbac` fixtures in `acceptance-azurerm.yml`. Add the cross-service fixture only if cross-type references need coverage |

## Pitfalls

- Malformed JSON is 400 `InvalidRequestContent` as JSON. A 500 with `text/plain` breaks every SDK error parser.
- Never map an unknown error code to 500. A client fault with an unmapped code hides the real cause; return the declared status.
- Missing api-version is 400. Accepting it hides client bugs that real ARM rejects.
- A duplicate role assignment is 409 `RoleAssignmentExists`. Asserting 400 would be wrong.
- Per-service not-found codes (`VirtualNetworkNotFound`, `SubnetNotFound`, `VMNotFound`) are not ARM. Use `ResourceNotFound`, and `ParentResourceNotFound` for a missing parent `(verify)`.
- 404 messages name the right scope. "Under resource group" is wrong for subscription-scope resources.
- `If-Match` must never be required on routes azurerm deletes without it. A required header breaks `terraform destroy`.
- A bare 202 with no `Location` sends azurerm into its own poller (about 20 s) or stalls it. Return a terminal 200 or 201.
- `reference()` on an unsupported type fails the deployment in real ARM. A null output with a `Succeeded` deployment is wrong.
- A running deployment is unobservable in a sync engine. Do not write a cancel-while-running test until deployments are async.
- Unrouted requests are not `EndpointNotFound`. Real ARM uses `InvalidResourceType` or `NoRegisteredProviderFound` `(verify)`.
- `GET` on an unknown provider namespace must not report `Registered`.
- A HEAD 404 has no body. Assert the status only.
- Single-subscription harnesses force unique RG names per test. Do not assume subscription isolation.
- Never commit private keys or certificates for client trust. Generate them per run.

## Dropped

| ID | Reason |
|---|---|
| X22 | Opt-in long-running operations are out of scope: ADR-0006 makes the engine sync-only (no 202, no `Azure-AsyncOperation`, no `Location`). A future async type is gated on a client that fails without 202; none is known. The SDK smoke rows cover poller behaviour against sync responses |
| X43 | What-if needs asynchronous deployments (real contract is 202 plus polling) and a diff engine. Out of scope per ADR-0006 and the deployments wave (T5) in [SPIKE](../SPIKE.md) |
| X50 | Management groups are a service in their own right (root group, subscription placement, descendants). Belongs on a management-group page; only the generic parent-existence rule applies to this page |
