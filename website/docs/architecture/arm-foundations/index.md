# miniblue ARM architecture review

Reviewed: `main` @ d86ed0d. Scope: control-plane foundations ahead of roughly 25 new ARM types, APIM, and azapi-heavy use. Review only. No repo changes.

## Verdict

**Evolve the engine. Don't rewrite it.** The brief's hypothesis is mostly right, but it has the wrong top risk.

1. **The top risk is the store value contract, not missing hooks.** Memory stores Go structs by reference. File and Postgres hand back decoded `map[string]any`: file does so after a restart, Postgres always. Handlers type-assert structs, so after a `PERSISTENCE=1` restart, or on Postgres, they panic or silently drop data today. Blob payloads are not persisted at all. The test suite cannot see any of it. Every new type adds to this. Fix it first (ADR-004).
2. **The engine has no transport-independent entry point.** It is an `http.Handler` and nothing more. Deployments therefore re-dispatch over `httptest`. RBAC and policy have nowhere to sit, and hooks are blind (no current doc, untyped errors, outside the write lock). Add `Engine.Do` (ADR-003).
3. **Coexistence is cheap if it is mechanically guarded.** It is not a long, risky period if three guards land in Phase 1: a legacy ARM route freeze, a boot-time engine/chi overlap panic, and scope cleaners for RG cascade. With those in place, migration becomes optional per type rather than a deadline. Data-plane handlers never migrate. vm, aks and sites may stay bespoke indefinitely (ADR-007).
4. **Remove machinery, don't add it.** Drop the engine's list index in favour of a prefix scan. Make api-version lists metadata, not gates. Unknown types stay strict, and passthrough is designed but not built until someone needs it.
5. **Fail closed, everywhere it matters.** Engine and RBAC store reads return errors as 500s, never "not found". Unsupported policy rules are rejected, never silently non-matching. State with an old schema stops startup with a reset message, never a silent misread.
6. **Persisted state is disposable test data.** A schema version stamp replaces data migrations, which removes a whole class of per-type code.

There are about 6 seams to add, not 8: `Do`/`Resolve`, Current-aware hooks under the lock, PatchMode, parent existence, Authorizer/Admitter, and Actions (Actions only lands with its first consumer).

## Documents

| Doc | Decision |
|---|---|
| [ADR-0001](adr/0001-single-arm-pipeline.md) | The engine is the single ARM pipeline for registered types. Guards make coexistence safe. Deployments call `Dispatcher.Put` in-process. |
| [ADR-0002](adr/0002-unregistered-types.md) | Strict 404 by default. Opt-in passthrough is designed but deferred until a concrete consumer exists. |
| [ADR-0003](adr/0003-pipeline-phases-and-hooks.md) | `Engine.Do`, phase order, `Request`, hooks under the lock, Authorizer/Admitter seams. |
| [ADR-0004](adr/0004-storage-contract.md) | Uniform JSON values on every backend, `Scan`, no list index, scope cleaners, a schema stamp instead of migrations. |
| [ADR-0005](adr/0005-type-metadata-and-conformance.md) | Hand-written registration, api-versions as metadata, `armtest` conformance suite. |
| [ADR-0006](adr/0006-sync-lro-and-errors.md) | Sync only, skip-polling header, `azerr.Error` taxonomy. |
| [ADR-0007](adr/0007-migration-strategy.md) | Strangler pattern, a per-type recipe, waves, exit criteria. |
| [ADR-0008](adr/0008-rbac-and-policy-minimum.md) | Opt-in RBAC and policy enforcement, the minimum that is useful. |
| [Slice 1](slices/01-role-assignments.md) | Role assignments on the engine, plus opt-in enforcement. |
| [Slice 2](slices/02-apim-consumption.md) | APIM Consumption control plane. |
| [PLAN](PLAN.md) | Phased PR plan with decision gates. |
| [SPIKE](SPIKE.md) | The earlier spike: resource ranking, test tiers, APIM provider findings. Partly superseded; its §0 lists what changed. |
| [Scenario catalogue](catalogue/index.md) | About 490 real-Azure scenarios per resource type, mapped to the engine, with engine lessons and a porting recipe. The acceptance list for each migration PR. |

ADRs are written to be commit-safe. Suggested home: `website/docs/architecture/adr/`.

## Findings

| # | Sev | Where | Finding | Fix |
|---|---|---|---|---|
| 1 | HIGH | `resourcegroups/handler.go:105`; `blob/handler.go:568,613,653,755,782` | `v.(ResourceGroup)` and `v.(Blob)` panic after a `PERSISTENCE=1` restart (`file.go:39` decodes to maps) and always on Postgres. | ADR-004: `store.Decode[T]`, uniform JSON. |
| 2 | HIGH | `acr:224`, `keyvault:574,654`, `appconfig:119`, `dbpostgres:264`, `blob:212,353` | `if x, ok := v.(T); ok` silently drops records in the same conditions. Lists come back short. | Same as #1. |
| 3 | HIGH | `store/memory.go:177-182` | `ListKeysByPrefix` returns **all** keys for file and Postgres. Consumers are `servicebus/handler.go:198` and `vm/references.go:189`. | `Backend.Scan(prefix)`, with every prefix op derived from it. |
| 4 | HIGH | `blob/handler.go:30` | `Blob.Content` is `json:"-"`. Every blob is empty after a `PERSISTENCE=1` restart and on Postgres. Any JSON-encoding store fix makes it fail in memory too. | `json:"content"` plus a restart test, **before** the byte-KV flip (ADR-004). |
| 5 | HIGH | `store/file.go:33-36,39` | A snapshot that fails to parse logs, starts empty, and autosave then overwrites it. Per-value decode errors are ignored. | Fail startup and leave the file untouched (ADR-004). |
| 6 | HIGH | `resourcegroups/handler.go:141-195,170,173` | Hard-coded `DeleteByPrefix` list. keyvault and `arm:` keys are missing. Imports aks and vm for cleanup (dependency inversion). | Scope cleaners plus `engine.DeleteScope` (ADR-004). |
| 7 | HIGH | `.github/workflows/ci.yml:47` | CI runs `./tests/...` only. Unit tests under `internal/` never run in CI. No race detector. Nothing exercises persisted state. | `go test ./...`, race on `internal/`, and a restart round-trip test. |
| 8 | MED | `store/postgres.go:96,143` | `LIKE` prefix is unescaped (`_` and `%` in names). `CountByPrefix` (120) escapes, so behaviour is inconsistent. No prefix index. | Shared escape helper, plus a `text_pattern_ops` index. |
| 9 | MED | `store/memory.go:142,149`; `file.go:51` | `Set` swallows errors. `SetIfNotExists` is not atomic. Values are stored by reference, so mutating after `Get` races with `Save`'s marshal. | Byte-KV backends, `SetNX`, error-returning `Lookup` and `Scan` for engine code. |
| 10 | MED | `server/server.go:177-186` | Routing correctness depends on middleware order. The authz middleware catches every `/providers/microsoft.authorization/` path. Nothing detects overlap. | Boot-time overlap guard and route freeze (ADR-001). |
| 11 | MED | `deployments/handler.go:188,214-218` | Missing apiVersion defaults to `2023-01-01`. Re-dispatch uses httptest. Nested ids are ad hoc. `dependsOn` is ignored. | `Dispatcher.Put` in-process, 400 on missing apiVersion, topo sort (ADR-001). |
| 12 | MED | `arm/engine.go:87-95` | Validate and Defaults can't see the stored doc, run outside the lock, and every error becomes 400. | ADR-003 hooks. |
| 13 | MED | `auth/handler.go:60`; `identity/handler.go:~299` | Every client token carries the same `oid`. MI tokens have no `oid`. Two JWT builders. Blocks any useful RBAC. | One `internal/token` package with a per-client oid (ADR-008). |
| 14 | MED | `authorization/handler.go:31,206,231` | Role assignment `createdOn` is regenerated on every PUT (drift). No 409 on duplicate. List is exact-scope only. The key is case-sensitive. Contributor has no `notActions`, so it is the same as Owner. | Slice 1, ADR-008. |
| 15 | MED | `resourcegroups/handler.go:215` | `/resources` always returns `[]`. | Engine-backed (ADR-004). |
| 16 | LOW | `arm/store.go` | The list index is a single value per parent and type: a hot row with no sub-wide list. | Drop it and use a prefix scan. |
| 17 | LOW | `azerr` | No target or details. Not an `error` type. | ADR-006. |
| 18 | LOW | `go.mod` | testcontainers (and the MySQL driver graph) sits in the main module for one integration test dir. | Optional separate module for `tests/integration`. |
| 19 | LOW | `services/publicip` | Global counter, ignored decode errors, 202 without Location. | Wave 2 migration fixes it for free. |
| 20 | INFO | AzureHeaders | No `X-Go-Azure-Sdk-Skip-Polling-Delay`, so azurerm waits about 10 s per write. | One-line header (ADR-006). |

From the spike, confirmed at d86ed0d. Each is a standalone `fix` PR with no dependencies:

| # | Sev | Where | Finding | Fix |
|---|---|---|---|---|
| 21 | MED | `redis/handler.go:99` | PUT ignores the request body. `location`, `tags` and `sku` are dropped, so azurerm and azapi see drift. | Decode the body and echo it, keeping generated values from the stored doc. |
| 22 | LOW | `loadbalancer/handler.go:156,188,219,238` | `resourceGuid` is regenerated on every PUT. | Keep it from the stored doc. |
| 23 | LOW | `keyvault/handler.go:280` | Vault PUT has no name validation (`vaultNamePattern` exists at `:456`) and no RG check. | Validate the name; 404 `ResourceGroupNotFound`. |
| 24 | LOW | `website/docs/services/parity.md` | Lists App Service as unimplemented (`sites` exists) and App Config as having no labels (it has them). | Update the page. |

## Open verify items

These are tagged `(verify)` in the ADRs. Resolve each one with a single real ARM call or a doc check, before the PR that depends on it.

- The real ARM error code for an unregistered type (`InvalidResourceType` vs `NoRegisteredProviderFound`).
- Role assignment error codes: invalid name, missing role definition, invalid principal, update not permitted.
- Role assignment list with no `$filter`: confirm it returns at, above and below scope. Confirm the `InvalidFilter` code.
- `PolicyDefinitionNotFound` on a policyAssignment with a bad definition id.
- APIM validation error code (`ValidationError`) and required-field rules.
- ARM `NotFound` and `AuthorizationFailed` message wording.
- `%2F` and `%3B` handling. `ParseID` uses the decoded `r.URL.Path`. `;rev=` works decoded, but an encoded `/` inside a name would split. Consider `RawPath` if a type needs it.
- A user-assigned identity in `userAssignedIdentities` that does not exist: rejected (which code) or echoed (ADR-0003).
- A template resource without `apiVersion`: confirm `InvalidTemplate` (ADR-0001).

Resolved during review:

- Built-in roles carry `permissions[]` (Contributor lacks `notActions`).
- `principalType` is echoed, or `""` when absent.
- `arm.ID` parses extension resources at any resource scope.
- The file snapshot is already `map[string]json.RawMessage` on disk (`file.go:32`), so byte-KV values load from existing snapshots without a format change.
