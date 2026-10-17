# ADR-0007: Migration strategy (strangler)

Status: Proposed

## Context

There are 37 service directories. Most mix ARM control plane, data plane and provider-level operations. Sizes run from `policy` (13 lines) to `vm` (1907). Store access is spread across 424 call sites.

A big-bang move is out (ADR-0001). The risk is a long coexistence period, so it has to be cheap and mechanically safe.

## Decision

### Rules

1. **After gate G1, no new chi ARM routes.** The legacy route freeze test enforces this (ADR-0001). New types go on the engine.
2. **Data plane never migrates.** blob, queue, table, KV secrets/keys/certs, appconfig KVs, servicebus messaging, ACR registry API, cosmos data, auth and metadata stay chi.
3. **Migration is per type and optional.** A type moves when it is touched for fidelity, for RBAC or policy coverage, or as part of its wave.
4. **State changes bump the schema version.** Persisted state is disposable test data. It is reset, never migrated, and never silently misread (ADR-0004 §5).

### Per-type recipe (the PR checklist)

1. Add `Register` in `internal/services/<rp>/types.go`: Type, Scopes, Patch mode, Provisioned.
2. Add an armtest `Sample` in `tests/armsamples_<rp>_test.go`.
3. Port the validation, defaults and actions into hooks. Read through `r.Store`. Copy generated values from `r.Current`.
4. Delete the chi ARM routes and their lines in `legacy_arm_routes.txt`. Keep the data-plane and provider-level routes.
5. Bump `store.SchemaVersion` and add a CHANGELOG line: `state reset required: <type> moved to the ARM engine`.
6. Remove the type's prefixes from the package's RG cleaner. Delete the cleaner if it is now empty.
7. Existing `tests/` stay green. Any intentional behaviour change (fidelity fix) is listed in the PR body and the CHANGELOG. A test that PUTs without creating its RG now gets 404 `ResourceGroupNotFound`: fix the test setup, not the engine.
8. PR title: `refactor(<rp>): serve <type> via arm engine`. Behaviour changes go in a separate `fix(<rp>): ...` commit.

### Waves

| Wave | Content | Why here |
|---|---|---|
| 0 | Foundation: store contract, CI, azerr, guards, `Engine.Do` (PLAN phases 0 and 1) | Everything else depends on it |
| 1 | Slices: roleAssignments, APIM Consumption, policyAssignments | Prove hooks, seams and new-type cost |
| 2 | Simple leaves: publicip, dns (zones plus record sets), eventgrid, redis, nsg plus rules, identity (userAssignedIdentities ARM), disks | Low coupling. They exercise the recipe and migrations. |
| 3 | Platform: resourcegroups and subscriptions on the engine, deployments on `Do` with `dependsOn`, `/providers` from the registry | Enables `ScopeExists` without a callback, and in-process deployments |
| 4 | Cross-referencing control plane: network (vnets/subnets), networkinterfaces, loadbalancer, appgw, storageaccounts ARM, keyvault ARM, acr ARM, appconfig ARM, dbpostgres, dbmysql, sqldb, cosmosdb ARM, servicebus ARM, aci | Need `Reader` cross-lookups and Actions (`listKeys`) |
| 5 | Heavy: vm, compute, aks, sites, apps, functions | **Gate G5**, below |

**Gate G5.** Migrate only the ARM shell of a wave-5 type when one of these holds:

- a fidelity bug needs engine behaviour (PATCH, cascade, sub-wide list);
- or enforcement coverage is requested and the legacy guard (ADR-0008) is not enough.

Otherwise the type stays bespoke. A bespoke type then:

- registers a scope cleaner;
- stays in the freeze file;
- is covered by `casemirror`.

### Tracking

Progress is the line count of `legacy_arm_routes.txt` plus the number of cleaners. No separate tracker.

### Exit

- `casemirror.go`, the authz catch-all middleware, the RG hard-coded list and the HTTP dispatcher fallback are deleted once their last consumer migrates.
- If wave 5 stays bespoke, `casemirror` remains, serving only those routes. That is acceptable.

## Consequences

- Coexistence has no deadline and no cliff. Every intermediate state is tested and shippable.
- The freeze file and the cleaners make the remaining legacy surface explicit.
- Wave work is mechanical enough for lesser agents to do with the recipe alone.

## Rejected alternatives

- **Migrate everything before adding new types:** it delays APIM and the new types for weeks of no-op churn.
- **Never migrate, and add new types only on the engine:** RBAC, policy, cascade and sub-wide list would stay split forever. Waves 2 to 4 are cheap with the recipe.
- **Migrate by package rather than by type:** the large packages (`network`, `vm`) would become unreviewable PRs.
- **A compatibility layer that serves legacy keys through the engine:** read fallbacks forever, to preserve disposable data.
