# ADR-0003: Pipeline phases and hooks

Status: Proposed

## Context

`internal/arm` is an `http.Handler` and nothing else. In `engine.go:74-119`:

- `Validate(id, body)` and `Defaults(id, body)` run **before and outside** the write lock;
- they cannot see the stored document;
- every error becomes 400 (`engine.go:87`).

That blocks:

- defaults that must be stable across PUTs (generated URLs, principalIds, `createdOn`);
- update rules ("principalId cannot change") and uniqueness checks (409);
- typed errors (404 parent, 409 conflict);
- RBAC and policy;
- in-process callers such as deployments.

## Decision

### Transport-independent core

```go
package arm

type Op uint8

const (
    OpGet Op = iota
    OpList
    OpPut
    OpPatch
    OpDelete
    OpAction
)

type Request struct {
    Op         Op
    ID         ID         // instance id; for OpList the parent scope
    Type       *Type
    APIVersion string
    Action     string     // OpAction only
    Query      url.Values
    Principal  Principal
    Body       map[string]any // decoded request (UseNumber)
    Current    map[string]any // stored doc; nil on create
    Doc        map[string]any // proposed doc; hooks mutate this
    Store      Reader
}

type Reader interface { // lock-free reads; backend errors are returned, never swallowed
    Get(id ID) (map[string]any, bool, error)
    Children(parent ID, typeName string) ([]map[string]any, error)
    Under(scope ID, typeName string) ([]map[string]any, error) // subscription or RG wide
}

// Resolve does routing and api-version presence. ok=false means "not ours".
func (e *Engine) Resolve(method, path string, q url.Values) (r *Request, ok bool, err error)
func (e *Engine) Do(ctx context.Context, r *Request) (status int, body any, err error)
func (e *Engine) ServeHTTP(w http.ResponseWriter, r *http.Request) // Resolve, decode, Do, write
```

The HTTP adapter stays thin, about 40 lines. Deployments and tests call `Resolve` and `Do` directly.

### Phase order

| # | Phase | Runs | Failure |
|---|---|---|---|
| 1 | route | `Resolve` | not ours, so fall through |
| 2 | api-version | `Resolve` | 400 `MissingApiVersionParameter` or `InvalidApiVersionParameter` (malformed only, see ADR-0005) |
| 3 | decode | adapter | 400 `InvalidRequestContent` |
| 4 | authorize | `Authorizer`, no lock | 403 `AuthorizationFailed` |
| 5 | lock, load `Current`, scope and parent existence | `ScopeExists`, `Reader` | 404 `ResourceGroupNotFound`, `ParentResourceNotFound`, `ResourceNotFound` |
| 6 | shape | engine | 400 (envelope, name/id mismatch). PATCH merge per `Type.Patch`. |
| 7 | admit | `Admitter` (PUT and PATCH) | 403 `RequestDisallowedByPolicy` |
| 8 | validate | `Hooks.Validate` | the returned `*azerr.Error`. A plain `error` becomes 400 `InvalidRequestContent`. |
| 9 | defaults | engine (`Provisioned`), then `Hooks.Defaults` | none |
| 10 | persist, unlock | store | 500 |
| 11 | after | `Hooks.After`, no lock | logged only |

GET, List and DELETE run phases 1, 2, 4, 5 and 10 (DELETE only).

Any `Reader` or store error in any phase gives 500 `InternalServerError`. Failures are never treated as "not found" or "no assignments".

Existence checks in phase 5 are deliberately narrow, so that coexistence with legacy types works:

- **Child types** (declared via `Children`): the parent doc must exist in the engine store. Otherwise 404 `ParentResourceNotFound`. The parent is always an engine type by construction.
- **RG and subscription scope:** `ScopeExists` is called. The server wires the RG check.
- **Extension resources at resource scope** (e.g. a roleAssignment on a legacy vault): **not existence-checked.** The scope may be a legacy resource that the engine cannot see. This is documented as a known deviation until a cross-system resolver is justified.

Authn is not a phase. A small, always-on middleware puts a `Principal` on `ctx` (from `oid`, else `sub`, read from the unverified bearer). It never fails a request.

### Hooks

```go
type Hooks struct {
    Validate func(ctx context.Context, r *Request) error
    Defaults func(ctx context.Context, r *Request) error // error only for read failures
    After    func(ctx context.Context, r *Request)
}
```

Rules, stated in the package doc:

- Validate and Defaults run **under the engine write lock** and see `Current`.
- They read via `r.Store`, or via a read-only resolver owned by the same package (e.g. legacy custom role definitions). They never write, and never call `Do`.
- Defaults must be **idempotent**. Generated values (ids, keys, URLs, timestamps) are copied from `Current` when it is present. armtest checks this (ADR-0005).
- After runs post-commit, outside the lock. Failures are logged, never returned.

### Type gains

```go
type PatchMode uint8

const (
    PatchMerge PatchMode = iota // RFC 7386 merge onto Current
    PatchTags                   // only tags may change
    PatchNone                   // 405
)

type ActionFunc func(ctx context.Context, r *Request) (status int, body any, err error)

type Type struct {
    // existing: Name, APIVersions, Scopes, Children, Hooks
    Patch       PatchMode                                           // replaces PatchTags bool
    Provisioned bool                                                // stamps properties.provisioningState=Succeeded
    List        func(ctx context.Context, r *Request) ([]map[string]any, error) // optional filter override
    Actions     map[string]ActionFunc                               // land with first consumer
}
```

- Existing registrations with `PatchTags: true` become `Patch: PatchTags`.
- Types relying on today's no-PATCH behaviour set `PatchNone` explicitly.

### Seams for RBAC and policy

```go
type Principal struct{ ObjectID, TenantID, AppID string }

func WithPrincipal(ctx context.Context, p Principal) context.Context
func PrincipalFrom(ctx context.Context) Principal

type Authorizer interface {
    Authorize(ctx context.Context, p Principal, action string, scope ID) error
}
type Admitter interface {
    Admit(ctx context.Context, r *Request) error
}

func New(reg *Registry, st *Store, opts ...Option) *Engine
func WithAuthorizer(a Authorizer) Option
func WithAdmitter(a Admitter) Option
func WithScopeExists(f func(ctx context.Context, scope ID) (bool, error)) Option
```

- A nil Authorizer or Admitter means allow. A nil `ScopeExists` means every scope exists.
- The server wires an RG check backed by the resourcegroups store until RGs move onto the engine.
- Action strings: `{type}/read`, `{type}/write`, `{type}/delete`, `{type}/{action}/action`. A list is `{type}/read` at the parent scope.

### Shared helpers (not Type fields)

```go
func CheckName(name string, re *regexp.Regexp, min, max int) error // 400 InvalidResourceName
func DefaultIdentity(doc, cur map[string]any, uami IdentityLookup)
func ChildID(parent ID, typeName, name string) ID

// IdentityLookup returns a user-assigned identity's principalId and clientId. Nil is allowed.
type IdentityLookup func(id string) (principalID, clientID string, ok bool)
```

`DefaultIdentity` semantics (exact, so implementations cannot drift):

- **No-op** when `doc.identity` is absent, or its `type` is `None`. The client's removal stands.
- **`SystemAssigned` in `type`:** set `principalId` and `tenantId`. Copy them from `cur.identity` if Current also had SystemAssigned, else generate a new UUID and use the default tenant.
- **`UserAssigned` in `type`:** for each key of `userAssignedIdentities`, set `principalId` and `clientId` from `uami(key)`. Azure echoes the identity resource's own ids, and Terraform configs often cross-reference them. If the lookup is nil or misses, copy from the same key in Current, else generate UUIDs. Whether Azure rejects a missing identity is (verify).
- Never adds an identity block the client did not send.

The server builds one `IdentityLookup` over the identity service. It reads legacy keys today and engine docs after wave 2, so callers never change.

### Headers and preconditions

- `If-Match` is ignored, and never required (azurerm APIM always sends `If-Match: *`).
- `If-None-Match: *` on an existing resource gives 412 `PreconditionFailed`.
- The engine never sends ETags until a type needs them.

## Consequences

- One function is the ARM contract. HTTP, deployments and tests all exercise the same code.
- Hooks can express real ARM rules (immutable fields, 409, stable generated values) without bespoke handlers.
- Writes are serialised across all engine types by one mutex. That is acceptable for an emulator, and keeps hooks race-free with no per-type locking.
- Hook authors must not do slow I/O under the lock. This is documented and code-reviewed.

## Rejected alternatives

- **Hooks outside the lock with optimistic retry:** more code and subtle bugs, for throughput nobody needs.
- **Authorize under the write lock:** a write racing a concurrent role revocation is indistinguishable from one that arrived a moment earlier. Real Azure RBAC is eventually consistent too. Authorizing before the lock keeps reads and writes on one code path.
- **Per-type mutexes:** cross-type validation (role definition lookup, parent existence) would need lock ordering.
- **A middleware chain (`func(next) Handler`) per phase:** flexible but opaque ordering. That is the current problem, moved inward.
- **`NamePattern`, `Immutable []string` or `Unique` fields on Type:** each one is a one-line hook. Declarative fields grow into a DSL.
- **A separate `arm.Error` type:** duplicates `azerr`. Extend `azerr` instead (ADR-0006).
- **An authn phase that rejects requests:** a non-goal. Real auth is out of scope, and a missing token resolves to the default principal.
