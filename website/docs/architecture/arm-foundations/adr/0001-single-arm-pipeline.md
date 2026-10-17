# ADR-0001: The engine is the single ARM pipeline

Status: Proposed

## Context

ARM requests reach miniblue through three uncoordinated paths:

1. **The engine middleware** (`server.go:181`) serves registered types (policyDefinitions today).
2. **The authorization middleware** (`server.go:186`) catches every `/providers/microsoft.authorization/` path.
3. **chi routes**, wrapped by `CaseInsensitiveARM(routeTrie)` (`server.go:85`, `casemirror.go`), for 37 service packages.

Correctness depends on middleware order. Nothing detects when a newly registered engine type shadows a chi route, or the reverse.

Deployments (`server.go:243`, `deployments/handler.go:214-218`) re-enter the server through `httptest` to create each template resource. That loses the caller principal, re-parses HTTP, and couples deployments to routing details.

## Decision

### Boundary

The engine owns every **resource instance, collection and instance action** of a registered type, at every scope.

Service packages keep:

- data-plane routes (blob, queue, table, KV secrets, and so on);
- provider-level non-resource operations: `checkNameAvailability`, `operationResults`, `locations/*/usages`, `deletedservices` if ever needed.

One rule, applied per type: once a type is registered, its chi ARM routes are deleted in the same PR.

### Guards (land in Phase 1, before any new type)

1. **Overlap guard.** At boot, `chi.Walk` the router. For every route:
    - substitute placeholders with sample values;
    - run `arm.ParseID` and `registry.Lookup`;
    - panic if a registered type matches.

    This is about 40 lines in `internal/server/guard.go`. It costs microseconds at startup.

2. **Legacy route freeze.** `internal/server/testdata/legacy_arm_routes.txt` lists the current chi ARM patterns, one per line, sorted. A test walks the router:
    - a pattern that is **not** in the file fails, with "register an arm.Type instead";
    - a pattern that is in the file but no longer routed passes, with a hint to delete the line.

    The file only shrinks. Provider-level operations are exempt, so service packages can still add them: a pattern is skipped when the first segment after `providers/{namespace}/` is `locations`, `operations` or `checkNameAvailability`.

3. **Scope cleaners** for RG and subscription cascade (ADR-0004). This removes the hard-coded delete list.

Engine middleware stays first. The authz middleware narrows over time to roleDefinitions only (ADR-0008).

### Deployments call the pipeline in-process

```go
// internal/arm
type Dispatcher interface {
    Put(ctx context.Context, id, apiVersion string, body map[string]any) (int, map[string]any, error)
}
```

The server provides the implementation:

- if `engine.Resolve` matches, it calls `engine.Do`;
- otherwise it falls back to the existing HTTP dispatcher, for legacy types only. The fallback copies the original request's `Authorization` header, so the principal survives the hop.

The fallback is deleted when the freeze file is empty of types used in templates.

Deployment handler changes:

- **Strip template-only keys** before the body is sent: `apiVersion`, `type`, `dependsOn`, `condition`, `copy`, `comments`, `scope`.
- **Missing `apiVersion`** gives 400 `InvalidTemplate` (verify code). Remove the `2023-01-01` default (`deployments/handler.go:188`).
- **Nested ids** come from one helper, `arm.ChildID(parent ID, typeName, name string)`. It is used for both PUT and `outputResources` (`deployments/handler.go:87,110`).
- **Order:** topo-sort on `dependsOn` (resourceId or name). On a cycle, return 400 `InvalidTemplate`.
- **Principal:** the caller principal flows via `ctx`, so RBAC and policy apply to template resources.

### End state

Delete each of these once its last consumer migrates (ADR-0007; `casemirror` stays if wave 5 stays bespoke):

- `casemirror.go`;
- the authorization catch-all middleware;
- the RG hard-coded delete list;
- the HTTP dispatcher.

## Consequences

- One place to add ARM behaviour. RBAC and policy cover every migrated type automatically.
- Legacy types keep working unchanged. Migration becomes per-type and optional (ADR-0007).
- Coexistence risk turns into two failing tests and a boot panic, not reviewer vigilance.
- Deployments become testable without HTTP and can enforce RBAC and policy.

## Rejected alternatives

- **Two systems by design** (bespoke handlers as first-class peers): every ARM behaviour (casing, PATCH, cascade, errors, RBAC, policy, sub-wide lists) gets implemented twice, forever.
- **Bespoke handlers as engine plugins** (an engine that routes to `http.Handler` per type): the engine gains a second dispatch model, and handlers still own the behaviours. Hooks are the plugin model.
- **Big-bang rewrite of all 37 packages:** 424 store call sites, high regression risk, no user-visible gain for data-plane code.
- **Engine after chi (NotFound fallback):** the reverse is safer. New types win, and the guard stops accidental shadowing either way.
- **Keep HTTP re-dispatch for deployments:** untestable without a server, and the principal survives only through a copied header. It is kept only as a fallback for legacy types.
