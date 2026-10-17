# ADR-0005: Type metadata and the conformance suite

Status: Proposed

## Context

There are about 25 new types to add, and each one needs the same ARM behaviours to be right:

- casing;
- PUT create vs replace;
- list at the parent scope and subscription-wide;
- PATCH semantics;
- cascade;
- the error envelope;
- `provisioningState`.

The engine registers types by hand (`registry.go`). `Register` panics on malformed metadata. `lookup` is private, and there is no `Types()`. `APIVersions` currently gates requests the wrong way round: an unknown version gives 400, while a missing version is accepted.

Only `tests/arm_policydefinition_test.go` exercises the engine, and that one test is bespoke.

## Decision

### Hand-written registration, one file per RP

```go
// internal/services/apim/types.go
func Register(reg *arm.Registry) {
    reg.Register(arm.Type{
        Name:        "Microsoft.ApiManagement/service",
        APIVersions: []string{"2024-05-01", "2022-08-01"}, // latest first; metadata
        Scopes:      arm.ScopeResourceGroup,
        Provisioned: true,
        Hooks:       arm.Hooks{Validate: validateService, Defaults: defaultService},
        Children: []arm.Type{
            {Name: "apis", Children: []arm.Type{{Name: "operations"}}},
            // ...
        },
    })
}
```

- The server calls each package's `Register`.
- Add `Registry.Types() []*Type`, sorted by name. It is used by armtest coverage and, later, by `/providers`.

### api-version is metadata, not a gate

- **Missing:** 400 `MissingApiVersionParameter`.
- **Malformed** (not `YYYY-MM-DD` with an optional `-preview` or similar suffix): 400 `InvalidApiVersionParameter`.
- **Any well-formed version is accepted.** `APIVersions` feeds `/providers` output and armtest, and nothing else.

Why: clients (azapi schema validation, the SDKs) already pin versions. Gating rots with every SDK bump, and miniblue serves one shape per type regardless of version.

### `internal/arm/armtest` (non-test package)

```go
package armtest

type Sample struct {
    Type    string            // full type name, e.g. "Microsoft.ApiManagement/service/apis"
    Scope   arm.Scope         // zero = first declared scope
    Body    map[string]any    // valid create
    Update  map[string]any    // valid replace, differs from Body
    Invalid map[string]any    // optional; Validate must reject it with 400
    Skip    map[string]string // check name -> reason (reason required)
}

func Run(t *testing.T, h http.Handler, reg *arm.Registry, samples []Sample)
func Coverage(t *testing.T, reg *arm.Registry, samples []Sample) // every registered type has a Sample
```

- `h` is the **full server handler**, so middleware order, casing and headers are covered.
- Parents are created from their own Samples. The RG is created with a PUT on the RG endpoint.
- Names are unique per check, so checks share one server, and `t.Parallel()` is safe.
- **There is no Options struct.** Expectations derive from `Type` metadata: `Patch` mode, `Provisioned`, `Children`, and `Scopes`.

Checks:

| Check | Asserts |
|---|---|
| PutCreate / PutReplace | 201 then 200. Replace drops fields absent from `Update`. `id`, `name` and `type` stay canonical. |
| Get / GetMissing | 200 round trip; 404 `ResourceNotFound` with the envelope |
| UrlCasing | Upper-cased path segments resolve to the same resource. Response casing is preserved. |
| List / ListSubscription | `{"value":[...]}` at the parent. Subscription-wide for RG-scoped top-level types. |
| Delete / DeleteMissing | 200, then 204 (or 200/204 per type) |
| MissingAPIVersion | 400 `MissingApiVersionParameter` |
| NameIDMismatch | 400 |
| BadBody | Non-JSON gives 400 `InvalidRequestContent`. `Invalid` gives 400. |
| Patch | Per mode: merge result, tags-only, or 405 |
| ParentMissing | Child PUT without a parent gives 404 `ParentResourceNotFound` |
| ChildCascade | Deleting the parent removes its children |
| RGCascade | Deleting the RG removes the resource |
| PrefixSibling | Deleting `x` leaves `x2` |
| DefaultsIdempotent | PUT the same body twice; the GETs are equal apart from `updatedOn`-like fields declared in `Skip` |
| ProvisioningState | `properties.provisioningState == "Succeeded"` when `Provisioned` |
| ErrorEnvelope | Every 4xx has `{"error":{"code","message"}}` and `x-ms-request-id` |

Where tests live:

- `tests/arm_conformance_test.go` calls `Run` and `Coverage` with every RP's samples.
- The samples sit beside it, in `tests/armsamples_<rp>_test.go`.
- The engine-level concurrency test stays as a single test in `internal/arm`.

The cost of a new type is the registration, any hooks, and one Sample of about 15 lines. Bespoke tests are written only for hook logic.

## Consequences

- Uniform behaviour is enforced, not reviewed. "Lesser agents" can add types safely.
- The coverage test fails if a type is registered without a sample.
- Run time scales linearly with the number of types. Expect well under 1 s for about 60 types in-process.

## Rejected alternatives

- **Codegen from the azure-rest-api-specs repo or bicep-types:** large inputs, a build step, and the generated validation is still not behaviour. Hand-written types are about 20 lines each.
- **Loading bicep-types at runtime:** tens of MB of data plus startup cost.
- **api-version allow-list gating:** rots, has no behavioural payoff, and clients already validate.
- **A strict flag for gating:** YAGNI.
- **Per-type hand-written CRUD tests:** duplicative, and they drift from each other.
- **An `armtest.Options` struct:** a second source of truth beside `Type`.
