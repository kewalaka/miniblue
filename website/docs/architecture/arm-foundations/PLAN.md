# Phased PR plan

Action small tasks in sets.  Each PR uses a conventional-commit title, and keeps `go test ./...` green. Sizes: S (under 200 lines), M (200 to 600), L (over 600). A PR may start once everything in its Depends column has merged. Rows with no dependency can run in parallel.

## Phase 0: Hygiene (no behaviour change for users)

| # | PR | Size | Depends | Notes |
|---|---|---|---|---|
| 0.1 | `ci: run go test ./... with -race on internal packages` | S | | `ci.yml:47` runs `./tests/...` only. |
| 0.2 | `fix(blob): persist blob content across restarts` | S | | `Blob.Content` tag becomes `json:"content"`. Adds the restart round-trip test (create RG, blob with upload, copy and block commit, KV, ACR, appconfig and pg server; Save; reload; GET and list). A snapshot that fails to load fails startup. This replaces a backend CI matrix. |
| 0.3 | `fix(store): add Decode and GetAs; fix struct-asserting call sites` | M | | Fix every site from README findings 1 and 2, and grep for `.(` on `store.Get` results. |
| 0.4 | `fix(store): byte-KV backends with uniform JSON values` | M | 0.2, 0.3 | ADR-0004 §1: `Scan`, `Lookup`, `SetNX`, `Set` returns error, `ListKeysByPrefix` fixed. The restart test must pass unchanged. Value shapes do not change, so there is no schema bump. Fix any sites the suite surfaces in this PR. |
| 0.5 | `fix(store): escape LIKE prefixes and add prefix index` | S | | Postgres only. |
| 0.6 | `refactor(resourcegroups): cascade via scope cleaners` | S | | Legacy cleaners only. Removes the aks and vm imports and adds the missing keyvault cleaner. Engine docs are wired in 1.2. |
| 0.7 | `feat(azerr): typed Error with target and details` | S | | Existing helpers wrap it. |
| 0.8 | `feat(server): send skip-polling-delay header` | S | | One line in AzureHeaders. |
| 0.9 | `build: move tests/integration to its own module` (optional) | S | | Drops testcontainers from the main `go.mod`. |
| 0.10 | `feat(store): schema version stamp with reset-required startup error` | S | | ADR-0004 §5. Starts at 1, so state from current main is accepted. |

README findings 21 to 24 (Redis PUT body, LB `resourceGuid`, KV name validation, parity page) are standalone `fix` and `docs` PRs. They are not gated and need no schema bump.

**Gate G0:**

- CI runs the full suite with race.
- The restart round-trip is green, including blob content.
- The schema stamp is in place.
- Zero struct type-asserts on store values remain (one grep, recorded in the PR).

## Phase 1: Engine foundation

| # | PR | Size | Depends | Notes |
|---|---|---|---|---|
| 1.1 | `refactor(arm): transport-independent Engine.Do and Resolve` | M | 0.4, 0.7 | ADR-0003: `Request`, error-returning `Reader`, hooks under the lock with `Current`, azerr errors. The HTTP adapter is thin. |
| 1.2 | `refactor(arm): prefix-scan lists, subscription-wide list, scope delete` | M | 1.1, 0.6 | Drop `arm:i:` (leftover keys are ignored, no bump). `DeleteTree` always. `DeleteScope`, wired into RG delete ahead of the cleaners. RG existence via `WithScopeExists`. |
| 1.3 | `feat(arm): PatchMode with RFC 7386 merge default` | S | 1.1 | `PatchTags` bool becomes a mode. |
| 1.4 | `feat(arm): require api-version; versions as metadata; Provisioned` | S | 1.1 | Plus `CheckName`, `DefaultIdentity`, `ChildID`. |
| 1.5 | `test(arm): armtest conformance suite` | M | 1.2, 1.3, 1.4 | Plus `Registry.Types()`. The policyDefinitions sample is the first consumer. |
| 1.6 | `test(server): engine/chi overlap guard and legacy route freeze` | S | | Generate the initial `legacy_arm_routes.txt`. |
| 1.7 | `feat(arm): principal context and Authorizer/Admitter seams` | S | 1.1 | Nil means allow. Principal middleware. |
| 1.8 | `refactor(auth): single token package with per-client oid` | S | | The default client keeps `...0002`. MI tokens gain `oid`. CHANGELOG. |

**Gate G1 (sign-off by Stu):**

- policyDefinitions passes armtest through the full server handler.
- The hook signatures and the `Type` fields are frozen. Any change from here on needs an ADR-0003 amendment.
- Startup time and RSS at idle are within 5% of `main` (measure: `time` to first 200 on `/health`, and RSS after 10 s).

## Phase 2: Slices

The two slices run in parallel. Within each, follow the Depends column.

| # | PR | Size | Depends |
|---|---|---|---|
| 2.1 | `feat(authorization): serve roleAssignments via arm engine` | M | G1, 0.10 |
| 2.2 | `fix(authorization): contributor notActions; add User Access Administrator` | S | |
| 2.3 | `feat(authorization): opt-in RBAC enforcement` | M | 2.1, 2.2, 1.7, 1.8 |
| 2.4 | `feat(apim): consumption control plane via arm engine` | M | G1 |
| 2.5 | `test(apim): terraform azurerm and azapi consumption fixtures` | S | 2.4 |
| 2.6 | `feat(policy): policyAssignments and opt-in deny/audit enforcement` | M | G1 |

**Acceptance (scenarios).** Each PR ports the T1/T2 rows of its [catalogue](catalogue/index.md) page and lists the ids it ported:

- 2.1 to 2.3: [authorization](catalogue/authorization.md). Note the real-Azure 409 `RoleAssignmentExists` and the `type` (not `roleType`) role-definition property.
- 2.4, 2.5: [API Management](catalogue/api-management.md) and its [provider notes](catalogue/api-management-provider-notes.md). Tier A only. The T3 fixtures are nightly, never per-PR.
- 2.6: no harvested scenarios for assignments. Management-group scope for definitions is `G2-T32` ([management groups](catalogue/management-groups.md)).

**Gate G2:**

- The slices landed without new engine seams beyond ADR-0003. If they did need one, the ADR was amended first.
- Record the actual cost per type (lines for registration, hooks and sample). If it is above about 80 lines for a simple type, revisit helpers before Phase 4.
- Decide on the legacy `Guard` (ADR-0008). Build it only if enforcement on unmigrated types is requested. It is not a prerequisite for any later gate.
- **New types (the about 25 planned) may start after G2.** They are engine-only, using the recipe.

## Phase 3: Platform

| # | PR | Size | Depends | Notes |
|---|---|---|---|---|
| 3.1 | `refactor(deployments): dispatch in-process via Engine.Do` | M | G2 | HTTP fallback for legacy types forwards the original `Authorization` header. Strip template keys. 400 on missing apiVersion. `ChildID` for nested ids and `outputResources`. |
| 3.2 | `feat(deployments): order resources by dependsOn` | S | 3.1 | Topo sort. A cycle gives 400. |
| 3.3 | `refactor(resourcegroups): serve resource groups via arm engine` | M | G2 | `/resources` from the engine. `ScopeExists` becomes engine-native. RG delete becomes sync 200, so drop the `operationresults` 202. Bumps `SchemaVersion`. |
| 3.4 | `feat(server): derive /providers from registry` | S | G2 | Merge with the existing static list until legacy types register. |

**Acceptance (scenarios).** 3.1 and 3.2: [deployments](catalogue/deployments.md). 3.3: [resource groups](catalogue/resource-groups.md). 3.4: [subscriptions and providers](catalogue/subscriptions-providers.md). Conflicts to resolve first: management-group PUT/DELETE are LROs in real Azure but ADR-0006 is sync-only, and several real PUTs return 200 or 202 only (not 201), which needs a per-type status override (see [engine lessons](catalogue/engine-lessons.md)).

**Gate G3:**

- An ARM template with `dependsOn` across an engine type and a legacy type deploys.
- With enforcement on, RBAC and policy apply to the engine-type resources in that template, and the deny comes back as a deployment error. Legacy-type resources are not enforced unless the Guard was built (ADR-0008).
- Decide whether roleDefinitions move onto the engine. That needs a Get fallback for built-ins. Only proceed if it is cheaper than keeping the narrowed middleware.

## Phase 4: Migration waves (parallel per type, lesser agents)

Follow the ADR-0007 recipe exactly. One PR per type family, titled `refactor(<rp>): serve <type> via arm engine`.

- **Wave 2:** publicip, dns, eventgrid, redis, nsg, identity (ARM), disks.
- **Wave 4:** network, networkinterfaces, loadbalancer, appgw, storageaccounts (ARM), keyvault (ARM), acr (ARM), appconfig (ARM), dbpostgres, dbmysql, sqldb, cosmosdb (ARM), servicebus (ARM), aci.

`Type.Actions` lands with the first `listKeys` consumer, which is a reviewed seam addition.

Each wave PR cites its [catalogue](catalogue/index.md) page as the acceptance list (T1 as the `Sample`, T2 as `tests/arm_<x>_test.go`). The pages also record the bugs to fix in the same PR (for example Redis PUT ignoring the body, Cosmos container `partitionKey`, ACI `buildResponse` dropping fields).

**Gate G4 (per wave):**

- The freeze file and cleaner count shrank.
- No `Type` fields were added outside an ADR amendment.
- Every PR that moved a type bumped `SchemaVersion` and added a CHANGELOG reset line.

## Phase 5: Heavy types and retirement

- **Gate G5** applies per type: vm, compute, aks, sites, apps, functions. Migrate the ARM shell only on a fidelity or enforcement need (ADR-0007). Otherwise they stay bespoke.
- **Retirement PRs**, each `refactor(server): remove <x>` when its last consumer is gone:
    - `casemirror.go`;
    - the authz catch-all;
    - the HTTP dispatcher fallback.

## Deferred until a concrete consumer exists

- **Passthrough for unregistered types** (`MINIBLUE_ARM_PASSTHROUGH=1`, ADR-0002). The design is recorded so nobody reinvents it differently. Build it when a user brings an azapi config that needs a type nobody intends to register. It is an S-sized PR touching only the chi NotFound handler.
- **Legacy `Guard`** (ADR-0008).

## Proposed, not scheduled

Rows suggested by the [scenario catalogue](catalogue/engine-lessons.md) (secret projection, seeded singleton children, soft-delete seam, `NameTaken`/`ValidateLocation`, provider registration state, deployments validate/cancel/export, `$top`/`nextLink`, extra `armtest` checks). They are proposals: each needs an owner decision, and any seam needs an ADR amendment first. New resource types listed in the catalogue (SQL, Insights, Log Analytics, Event Hubs, management groups) are engine-only registrations and may start after G2.

## Definition of done

Three loops. The first one keeps CI and review cheap, so do not skip it.

**1. Inner loop (local, before every push).** Seconds to a couple of minutes.

- `go build ./... && go vet ./... && go test -race ./internal/...`, then `go test ./tests -run <Type>` for the area touched. CI does not run `internal/` tests until 0.1 lands, so this is the only place they run.
- New or migrated type: an armtest `Sample` and the T2 rows from its [catalogue](catalogue/index.md) page.
- Store changes: the restart round-trip test (0.2).
- No Terraform, Docker or e2e locally. Those are CI-only.

**2. PR CI (once per push).**

- Push when the inner loop is green. Then run `gh pr checks <n>` and wait for it. Do not poll or re-run to see if it passes.
- On a failure, reproduce it locally, fix it in one commit, push once. A new push cancels the run in progress.
- Terraform acceptance (`acceptance.yml`, `acceptance-azurerm.yml`) runs on a PR only when Go code, `go.mod`/`go.sum`, a Terraform fixture or example, or the workflow itself changes. It always runs on push to main. Docs-only PRs skip it.
- A slow fixture (API Management or similar) is never a per-PR job. Put it in a scheduled or manual workflow.

**3. Review loop (after CI is green).**

- The PR description names the PLAN row, lists the catalogue ids ported, and says which `(verify)` marks were resolved.
- A code-review agent checks:
    - the row's Depends were merged;
    - no `Type` fields, hook parameters or engine options were added without an ADR amendment;
    - every store error is checked;
    - `SchemaVersion` is bumped with a CHANGELOG line if the key or value shape changed;
    - the legacy route freeze file or cleaner count did not grow;
    - a `Sample` exists for each type touched.
- At most two review rounds. If it is still open after that, escalate to Stu rather than iterating.
- Stu reviews and signs off the gates (G0 to G5), not each PR.

## Rules for implementing agents

- Read ADR-0003, ADR-0005 and ADR-0007 before touching `internal/arm` or migrating a type.
- Do not add `Type` fields, hook parameters or engine options without an ADR amendment PR.
- Do not add chi ARM routes. The freeze test will fail.
- Every new or migrated type gets an armtest `Sample`. Bespoke tests are only for hook logic.
- Changing the key or value shape of an existing persisted type bumps `store.SchemaVersion`, with a CHANGELOG `state reset required` line. New types do not bump. Never write data migrations.
- Engine and migrated code check every store error. A read error is a 500, never "not found". No persisted field uses `json:"-"`.
- Mark any assumed Azure error code `(verify)` in code comments until it is confirmed.
