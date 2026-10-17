# Engine lessons

> Engine fit: design guidance for the shared engine helpers and the porting workflow (not a service). miniblue today: the engine serves `policyDefinitions` only; `armtest` and the legacy route freeze file do not exist yet. Suggested wave: PLAN 1.4 and 1.5 for the helpers, Phase 4 for the recipe.

This page turns the design lessons about shared ARM helpers into decisions for miniblue's engine. It does three things: states each lesson with the real Azure behaviour behind it, restates the porting recipe and test skeleton in current ADR vocabulary, and cross-checks every lesson against the ADRs and the PLAN so the gaps are visible. It proposes amendments and PLAN rows; it edits neither.

Authoritative sources: [ADR-0003 pipeline and hooks](../adr/0003-pipeline-phases-and-hooks.md), [ADR-0005 conformance](../adr/0005-type-metadata-and-conformance.md), [ADR-0006 sync and errors](../adr/0006-sync-lro-and-errors.md), [ADR-0007 migration](../adr/0007-migration-strategy.md), [PLAN](../PLAN.md). Scenarios that exercise these lessons are on [cross-cutting scenarios](cross-cutting.md).

Statements marked `(verify)` were not checked against the REST specs or provider source.

## Lessons

### Name validation

| Lesson | Detail |
|---|---|
| Validate on PUT, not only in checkNameAvailability | A helper that checks names only in the availability call lets invalid names through the create path. Real ARM rejects on PUT with 400 |
| The rule is per type | There is no universal name rule. Put the regex and length in the type's `Validate` hook through `CheckName(name, re, min, max)` (400 `InvalidResourceName`, ADR-0003). Declarative name fields on `Type` were rejected in ADR-0003 |
| Share the rule with the name check | The provider-level `checkNameAvailability` route and the PUT hook must use one regex per type, or the two drift |
| Error code varies by RP | `InvalidResourceName` is the default; some RPs use their own code `(verify)`. Pin the code per type in its Sample (`Invalid`) |

Constraints to confirm before asserting, all `(verify)`:

| Type | Constraint |
|---|---|
| Storage account | 3 to 24 characters, lowercase letters and digits, globally unique |
| SQL server | 1 to 63 characters, lowercase letters, digits and hyphens, globally unique |
| Service Bus namespace | 6 to 50 characters, letters, digits and hyphens, starts with a letter, globally unique |
| Virtual network, subnet | 2 to 64 characters |
| API Management service | per [Slice 2](../slices/02-apim-consumption.md) (`CheckName` with 1 to 50) |

### Id parsing

| Lesson | Detail |
|---|---|
| Parse structurally, not positionally | Position-based parsing breaks on extension resources, names equal to a keyword (`providers`) and mixed-case segments. `arm.ParseID` is the structural parser; keep it |
| Build ids from one helper | Nested ids come from `ChildID(parent, typeName, name)` for both the PUT response and `outputResources` (ADR-0001, PLAN 1.4). A builder pairs each type segment with a name segment and handles the tenant-level empty scope |
| Name casing is frozen at create | Lookups are case-insensitive; the response keeps canonical keywords and the creating PUT's name casing (ADR-0003) |
| Opaque names | Some names carry characters such as `;rev=` (APIM). Treat the segment as opaque after decoding; an encoded `/` inside a name is still open (SPIKE) |

### Error taxonomy

One envelope everywhere: `{"error":{"code","message","target"?,"details"?}}` as `application/json` through `azerr.Error` (ADR-0006, PLAN 0.7). Never default an unmapped code to 500, and never invent per-service not-found codes.

| Status | Codes in use or planned | When |
|---|---|---|
| 400 | `InvalidRequestContent`, `MissingApiVersionParameter`, `InvalidApiVersionParameter`, `InvalidResourceName`, `InvalidParameter` | bad body, bad or missing api-version, bad name, hook validation |
| 403 | `AuthorizationFailed`, `RequestDisallowedByPolicy` | RBAC or policy enforcement (ADR-0008) |
| 404 | `ResourceNotFound`, `ResourceGroupNotFound`, `SubscriptionNotFound`, `ParentResourceNotFound`, `InvalidResourceType` `(verify)` | missing resource, RG, subscription, parent, unregistered type |
| 405 | `MethodNotAllowed` | unsupported verb, `PatchNone` types |
| 409 | `Conflict` and type-specific codes such as `RoleAssignmentExists`; `InvalidResourceGroupLocation`; `MissingSubscriptionRegistration` | duplicates, immutable-field changes, unregistered provider |
| 412 | `PreconditionFailed` | `If-None-Match: *` on an existing resource |
| 500 | `InternalServerError` | store or reader failure (never treated as not found) |

Rules that follow:

- Missing parent is `ParentResourceNotFound`, not a per-service code (`VirtualNetworkNotFound`, `SubnetNotFound`) `(verify)`.
- 404 messages are scope-aware: "under resource group" only at RG scope.
- Hooks return `*azerr.Error`; a plain `error` becomes 400 `InvalidRequestContent` (ADR-0003 phase 8).
- Any `Reader` or store failure is 500, so an outage never looks like an empty list.

### List and paging

| Lesson | Detail |
|---|---|
| Shape | `{"value":[...]}` with `nextLink` only when the RP pages. Engine lists are sorted and prefix-safe (deleting `x` leaves `x2`) |
| Scope variants | Parent list, RG-wide and subscription-wide lists for the same type; `Reader.Under` serves the last two (ADR-0003, PLAN 1.2) |
| Paging is deferred | Clients that list follow `nextLink` if present, so omitting it is safe until a list is large. `$top`, `$skip` and `$filter` are RP-specific; add per type when a client needs them |
| Generic resource list | `GET .../resourceGroups/{rg}/resources` aggregates across types. It is `[]` today and becomes engine-backed at PLAN 3.3; `$filter=resourceType eq` is not planned |

### Secrets handling

| Lesson | Detail |
|---|---|
| Never echo secrets on GET | Admin passwords, keys, connection strings and similar fields are accepted on PUT and are never returned by GET. Keys come back only from a `POST` action (`listKeys`, `listSecrets`) |
| The engine stores what the client sent | There is no redaction step, so a typed model with a password field echoes it on GET. This is a documented deviation today, not Azure behaviour |
| A Defaults hook cannot fix it | Defaults output is persisted; stripping there loses the value an action later needs |
| Needed seam | Keep secrets in the stored document and project them out of GET, list and PUT responses; actions read the stored value. Declare the paths per type (the SPIKE called it `SecretPaths`). Requires an ADR-0003 amendment |
| Some secrets are masked, not removed | Named values and similar fields return a masked or empty value on GET `(verify)` per type |

### Soft-delete

| Lesson | Detail |
|---|---|
| Behaviour | Key Vault, App Configuration and API Management keep a deleted resource in a per-location `deleted...` collection. A recreate with the same name conflicts until purge. Purge of a purge-protected item is 409 `Conflict` `(verify)` |
| Engine today | DELETE hard-removes. Hooks never write (ADR-0003), so a hook cannot move a document into a deleted collection |
| Needed seam | A write-capable delete path, or a second registered type for the deleted collection plus an engine-owned move. Decide before the first soft-delete type migrates |
| Interim | Legacy soft-delete flows stay bespoke (ADR-0006, ADR-0007). `purge_soft_delete_on_destroy` is documented unsupported for APIM; the deleted-service GET is a strict 404 |

### POST actions

| Lesson | Detail |
|---|---|
| Instance actions are engine-owned | `listKeys`, `regenerateKey`, `listSecrets` belong in `Type.Actions`, which lands with its first consumer (ADR-0001, ADR-0003, PLAN Phase 4). Authorisation strings follow `{type}/{action}/action` |
| Provider-level operations stay chi routes | `checkNameAvailability`, `operationResults`, `locations/*`, `operations`, `deletedservices` (exempt from the freeze file, ADR-0001) |
| Subscription and deployment actions are not engine actions | subscription cancel and enable, deployment validate, cancel, exportTemplate and what-if stay bespoke |
| Unregistered action paths | Fall through today; with ADR-0002 they become a strict 404 |

### Other helper lessons

| Concern | Lesson | Today |
|---|---|---|
| Server-owned fields | Keys, creation times, generated GUIDs and URLs must survive a replacing PUT. Copy from `Current` in `Defaults`; Defaults must be idempotent | Covered by ADR-0003 and the DefaultsIdempotent check |
| Global name uniqueness | DNS-global names (storage, SQL server, Service Bus, Key Vault, ACR, APIM) conflict across RGs and subscriptions: 409 (storage uses `StorageAccountAlreadyTaken` `(verify)`) | No helper. A hook can scan with `Reader.Under`, but a cross-subscription check needs a store-wide lookup |
| Location | `location` is required on regional types (400) and must be in the type's list (400 `LocationNotAvailableForResourceType`); global types have no `location` | No helper. Each type would hand-roll it |
| Tags | Replace on PUT, default `{}`, PATCH per `PatchMode`. Limits of 50 tags, name up to 512, value up to 256 `(verify)` | Modes covered; limits not enforced |
| Singleton and default children | `default`, `current`, `$Default`, `RootManageSharedAccessKey` exist or are auto-created, so GET is 200 before any PUT | No seed mechanism (APIM Tier B needs one) |
| Provider registration | A per-subscription state; PUT under an unregistered namespace is 409 `MissingSubscriptionRegistration`; `az` auto-registers and retries | Only the namespace list is derived from the registry (PLAN 3.4); no state, no gate |
| Cross-resource side effects | NIC and private endpoint IP allocation against a subnet, availability set membership, subscription creation seeding RBAC | Out of engine scope: hooks are read-only. These stay bespoke |
| ETag | Optional per RP; mismatched `If-Match` is 412. Never require `If-Match` where azurerm deletes without it | Engine ignores `If-Match`, sends no ETags |

## Porting recipe

ADR-0007 holds the per-type checklist and is authoritative. The table below is the full mechanical sequence, with the steps ADR-0007 does not spell out marked "extra".

| # | Step | Where | Notes |
|---|---|---|---|
| 1 | Classify fit: passthrough, hooks, engine-gap, bespoke. If a gap needs an engine change, land it first with an `internal/arm` unit test and an ADR amendment | `internal/arm/*` | Gate G4: no new `Type` fields outside an ADR amendment |
| 2 | `Register(reg *arm.Registry)`: one `arm.Type` per top-level type; children via `Children` | `internal/services/<rp>/types.go` | `Name` must be `Namespace/type` with one slash; a malformed, duplicate or incomplete type panics (`internal/arm/registry.go:46-61`) |
| 3 | Mount before the engine, gated by the service filter | `internal/server/server.go:178-181` | `if serviceEnabled("<svc>", allowed) { <pkg>.Register(armTypes) }` precedes `arm.NewEngine` (extra) |
| 4 | Append the service name for `/health` | `internal/server/server.go:267-268` | Extra. The `/health` list is built from the same filter |
| 5 | Delete the chi ARM routes and their lines in `legacy_arm_routes.txt`; keep data-plane and provider-level routes | the old handler, freeze file | Freeze file lands with PLAN 1.6; it does not exist yet. Engine shadows chi, so legacy state would dangle |
| 6 | Provider-level chi routes for `checkNameAvailability` and similar | beside `Register` | Exempt from the freeze file. Share the name rule with the hook |
| 7 | Add an armtest `Sample` | `tests/armsamples_<rp>_test.go` | One Sample of about 15 lines; `Skip` needs a reason (ADR-0005) |
| 8 | Port validation, defaults and actions into hooks; read through `r.Store`; copy generated values from `r.Current` | `hooks.go` | Hooks never write and never call `Do` |
| 9 | Bespoke tests for hook logic and coexistence | `tests/arm_<name>_test.go` | See the skeleton below |
| 10 | Remove the type's prefixes from the package's RG cleaner | the package cleaner | Delete the cleaner if it is empty |
| 11 | Bump `store.SchemaVersion` and add a CHANGELOG line `state reset required: <type> moved to the ARM engine` | `internal/store`, `CHANGELOG.md` | ADR-0004 and ADR-0007 |
| 12 | Add the namespace to the provider list only if clients enumerate it | `internal/services/subscriptions/handler.go` | Optional; `Microsoft.Authorization` is already absent and policy works. PLAN 3.4 derives it later (extra) |
| 13 | Docs: services parity table and overview, a service page, `mkdocs.yml` nav, README service counts, CHANGELOG | `website/docs/services/*` | Extra. The contributing guide still documents the hand-written pattern `(verify)`; add the engine recipe in the first porting PR |
| 14 | Terraform acceptance when the type is reachable from azurerm or azapi | azapi: `tests/terraform/azapi/<case>/{main.tf,run.sh}` plus a job in `acceptance.yml`; azurerm: `tests/terraform/azurerm/<fixture>/main.tf`, a committed `.terraform.lock.hcl` (linux_amd64, darwin_arm64, windows_amd64) and a matrix entry in `acceptance-azurerm.yml` | Extra. `run.sh` asserts apply, no drift, a no-op second apply, destroy. Linux or WSL2 only |
| 15 | Behaviour changes in a separate `fix(<rp>)` commit; PR title `refactor(<rp>): serve <type> via arm engine` | git | ADR-0007 |

A test that PUTs without creating its RG now gets 404 `ResourceGroupNotFound`. Fix the test setup, not the engine.

### Integration-test skeleton

After ADR-0005 the generic behaviour moves into the armtest Sample, so the bespoke file shrinks to what a Sample cannot say. Helpers already in `tests/helpers_test.go`: `setupServer`, `doRequest`, `expectStatus`, `decodeJSON`, `decodeError`. `tests/arm_policydefinition_test.go` is the worked example (five tests).

| Test (file `tests/arm_<name>_test.go`) | Asserts | Covered by armtest instead |
|---|---|---|
| `TestArm<Thing>_Lifecycle` | PUT 201 then 200, mangled-case GET, list, DELETE 200 then 204, GET 404 | PutCreate, PutReplace, Get, UrlCasing, List, Delete |
| `TestArm<Thing>_RequestValidation` | the type's own `Validate` codes (typed 400, 409) | generic checks: MissingAPIVersion, NameIDMismatch, BadBody |
| `TestArm<Thing>_ScopeOnly` | a type registered at one scope falls through at the other scopes | none; keep |
| `TestArm<Thing>_ComputedFieldsStable` | specific computed fields (ids, keys, endpoints) stable across a second PUT, no azurerm drift | DefaultsIdempotent (generic) |
| `TestArm<Thing>_CoexistsWithHandWritten` | a hand-written neighbour on the same subscription still answers | none; keep |
| `TestArm<Thing>_ServiceFilterAndReset` | `SERVICES` excluding the service leaves the type unmounted; `/_miniblue/reset` empties it | none; keep |
| `TestArm<Thing>_CascadeOnResourceGroupDelete` | RG delete removes the resource | RGCascade |
| `TestArm<Thing>_Actions` | `listKeys` and similar: status, body shape, case-mangled path | none; keep once `Type.Actions` exists |

```go
// Signatures only; bodies follow tests/arm_policydefinition_test.go.
func TestArmThing_RequestValidation(t *testing.T)    // hook codes only
func TestArmThing_ScopeOnly(t *testing.T)            // falls through at other scopes
func TestArmThing_CoexistsWithHandWritten(t *testing.T)
func TestArmThing_ServiceFilterAndReset(t *testing.T)
func TestArmThing_Actions(t *testing.T)              // after Type.Actions
```

### Migration notes

Wave order and gates are in PLAN Phase 4 and ADR-0007. Keep these in view:

| Item | Note |
|---|---|
| Stays bespoke | data planes always; `keyvault`, network (inline plus child sync), `storageaccounts` (PUT must return 200 `(verify)`), `vm`, `aci`, `aks`, `sites`, `dbpostgres` unless a fidelity or enforcement need arises (ADR-0007 gates G4, G5) |
| Casing | Legacy handlers mirror client casing; engine types return canonical casing. A move is a listed behaviour change |
| Middleware order | The engine must stay ahead of authorisation middleware so extension scopes resolve |
| State | Moving a type changes the key space; bump `SchemaVersion` |
| Drift | Idempotent `Defaults` is the guard against azurerm plan drift; the acceptance fixture proves it |
| Cost | Each migrated type adds one Sample (fast) and, if reachable, one Terraform job (slow). Batch azapi fixtures if the count grows |

## ADR cross-check

"Covered" cites the ADR or PLAN row that already decides the lesson. "Gap" is a proposal, not a decision: an ADR amendment or a PLAN row for the owners to accept.

| Lesson | Already covered by | Gap (proposal) |
|---|---|---|
| Typed errors with `target` and `details`; generic codes | ADR-0006, PLAN 0.7 | none |
| Code for an unrouted request | ADR-0002 (strict 404, code open) | pin `InvalidResourceType` or `NoRegisteredProviderFound` `(verify)` in the PLAN row that implements ADR-0002 |
| Missing and malformed api-version | ADR-0005, ADR-0006, PLAN 1.4 | MalformedAPIVersion check is not in the ADR-0005 list; add it |
| Name validation per type in hooks | ADR-0003 (`CheckName`), PLAN 1.4 | negative Terraform fixture for name errors (see [client suites](client-suites.md)); share one rule between the hook and the availability route |
| Id parsing and builder | `arm.ParseID`; `ChildID` in ADR-0003 and PLAN 1.4 | extend the id tests with builder cases (tenant-level scope, unpaired segments) |
| RG and parent existence | ADR-0003 phase 5 | subscription existence needs a "known subscription" model; no PLAN row |
| Server-owned fields, stable defaults | ADR-0003 (`Current`, idempotent `Defaults`), ADR-0005 DefaultsIdempotent | none |
| Sub-wide list, prefix safety | ADR-0004, PLAN 1.2 | none |
| Paging and `nextLink` | none (SPIKE "Later") | PLAN row `feat(arm): $top and nextLink for lists`, triggered by the first type whose list needs it |
| RG `/resources` | PLAN 3.3 | `$filter=resourceType eq` is not covered; add to 3.3 or drop |
| RG cascade | ADR-0004, PLAN 0.6 and 1.2 | ScopeSurvivesRGDelete check |
| Secrets never echoed | none (SPIKE "Later", `SecretPaths`) | ADR-0003 amendment: declared secret paths projected out of responses; PLAN row `feat(arm): secret path projection` before the first secret-bearing type (SQL, APIM named values) |
| Soft-delete and purge | ADR-0006 and ADR-0007 keep legacy flows bespoke; APIM deletedservices is a strict 404 | ADR amendment for a write-capable delete path or deleted-collection type, before App Configuration or Key Vault migrates |
| POST instance actions | ADR-0001, ADR-0003 (`Type.Actions` with first consumer), PLAN Phase 4 | none |
| `checkNameAvailability` | ADR-0001 (provider-level chi route) | shared name-taken helper (next row) |
| Global-name uniqueness | none | helper `arm.NameTaken(...)` over a store-wide lookup, used by `Validate` and the availability route; PLAN row |
| Location validation | none | helper `arm.ValidateLocation(allowed...)` added to the ADR-0003 helper list |
| Tags limits | none | optional; verify limits first, then a shared `Validate` helper |
| `provisioningState` | ADR-0003 `Provisioned`, ADR-0005 ProvisioningState | none |
| Singleton and auto-created children | none (SPIKE "Later"; APIM page notes the need) | ADR amendment for a seed mechanism; PLAN row before APIM Tier B |
| Provider registration state and 409 gate | PLAN 3.4 (derive the list only) | PLAN row: stored per-subscription state, `unregister`, tenant `/providers`, optional 409 gate default off |
| Cross-resource side effects | ADR-0003 (hooks are read-only), ADR-0007 (wave 4 optional) | none; state explicitly that these stay bespoke |
| Sync only, no LRO | ADR-0006 | none |
| ETag and `If-Match` | ADR-0003 (ignored, never required) | opt-in ETag type flag only when a type needs it |
| Deployments beyond RG-scope CRUD | ADR-0001, PLAN 3.1 and 3.2 | validate, exportTemplate, cancel, HEAD, subscription, management group and tenant scope, operations have no PLAN row (T5 in the SPIKE); what-if is out of scope |
| Conformance additions | ADR-0005 (16 checks) | WrongMethod, ResponseHeaders, UnknownRoute, ScopeSurvivesRGDelete; see [cross-cutting](cross-cutting.md) |

### Proposed PLAN rows

Proposals only. Sizes are estimates; the PLAN owners decide numbering and dependencies.

| Proposed row | Size | Depends on | Trigger |
|---|---|---|---|
| `test(arm): extra armtest checks (MalformedAPIVersion, WrongMethod, ResponseHeaders, UnknownRoute, ScopeSurvivesRGDelete)` | S | 1.5 | with 1.5 |
| `feat(arm): ValidateLocation and NameTaken helpers` | S | 1.4 | first type needing either |
| `feat(arm): secret path projection` | M | 1.1 | first secret-bearing type |
| `feat(arm): seeded singleton children` | M | 1.1 | APIM Tier B or the first `default` child |
| `feat(arm): soft-delete seam` | M | 1.1 | first soft-delete type migrates |
| `feat(server): stateful provider registration` | M | 3.4 | `az` suite or a client that needs the gate |
| `feat(deployments): validate, cancel, exportTemplate, HEAD, scopes, operations` | M | 3.1 | after dispatch is in-process |
| `feat(arm): $top and nextLink` | M | 1.2 | first large list |

## Pitfalls

- A name rule enforced only in `checkNameAvailability` lets invalid names reach the store.
- A per-service not-found code is not ARM. Use `ResourceNotFound` and `ParentResourceNotFound`.
- Mapping an unknown error code to 500 turns client faults into server faults in every SDK.
- Echoing a secret on GET is not Azure behaviour, even when the typed model makes it free.
- A hook that writes under the engine lock breaks the ordering guarantees in ADR-0003. Needs that require writes need a seam, not a workaround.
- Requiring `If-Match` breaks `terraform destroy`, which deletes without it.
- Defaults that regenerate keys, GUIDs or timestamps on every PUT cause azurerm plan drift.
- A bare 202 with no `Location` stalls the azurerm poller. Return a terminal status.
- Moving a type without bumping `SchemaVersion` leaves dangling legacy state that is silently misread.
