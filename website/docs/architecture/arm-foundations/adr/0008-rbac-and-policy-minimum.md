# ADR-0008: RBAC and Policy, the useful minimum

Status: Proposed

## Context

**RBAC today** (`internal/services/authorization/handler.go`):

- Nine built-in role definitions are synthesised with `permissions[]` (`:30-38`). Contributor has empty `notActions`, so it is the same as Owner.
- Role assignments are CRUD only, stored at `authorization:roleAssignment:<scope as sent>:<name>`. The key is case-sensitive.
- `createdOn` is regenerated on every PUT (`:206`), so there is drift.
- There is no duplicate check, the list is exact-scope only, and a delete of a missing assignment returns 404.
- There is no enforcement.

**Tokens.** `auth/handler.go:60` gives every client token the same `oid`/`sub` (`00000000-0000-0000-0000-000000000002`). MI tokens (`identity/handler.go:~299`) carry `sub` = principalId and no `oid`. There are two separate JWT builders.

**Policy.** Only `policyDefinitions` exists, as an engine type. There are no assignments and no evaluation.

The useful target: let users test "this principal can't do that" and "this policy denies that" locally, without emulating Azure's full semantics.

## Decision

### RBAC

**Role assignments move onto the engine** (Slice 1). roleDefinitions stay in the narrowed authz middleware until a Get-fallback seam for built-ins is justified (gate G3).

**Built-ins.** Keep them synthesised. Fix Contributor `notActions` to:

- `Microsoft.Authorization/*/Delete`
- `Microsoft.Authorization/*/Write`
- `Microsoft.Authorization/elevateAccess/Action`

Add **User Access Administrator** (`18d7d88d-d35e-4fb5-a5c3-7773c20a72d9`: `*/read`, `Microsoft.Authorization/*`, `Microsoft.Support/*`).

**Enforcement** is off by default. `MINIBLUE_RBAC=enforce` wires an `arm.Authorizer`:

```go
// internal/services/authorization/enforce.go
type RoleResolver func(id string, scope arm.ID) (map[string]any, bool, error)

type Enforcer struct {
    Store arm.Reader   // engine roleAssignments
    Roles RoleResolver // built-ins, plus custom roleDefinitions wherever they live today
    Admin string       // MINIBLUE_RBAC_ADMIN, default 00000000-0000-0000-0000-000000000002
}

func (e *Enforcer) Authorize(ctx context.Context, p arm.Principal, action string, scope arm.ID) error
```

`Roles` is `Handler.ResolveRoleDefinition`. Custom role definitions stay at legacy keys until they migrate, and `arm.Reader` cannot see those keys. Injecting the resolver keeps the Enforcer indifferent to where definitions live. When roleDefinitions migrate, only the resolver body changes.

1. If `p.ObjectID == e.Admin`, allow (implicit Owner at `/`). Default usage keeps working with enforcement on.
2. Walk `scope` and then each `Parent()` up to the tenant. At each level, collect roleAssignments where `principalId == p.ObjectID`.
3. Resolve each roleDefinitionId with `e.Roles` (built-in or custom). An unresolvable definition grants nothing.
4. Allow if any permission block has an `actions` match and no `notActions` match.
5. Otherwise return 403 `AuthorizationFailed`:

    > The client '{oid}' with object id '{oid}' does not have authorization to perform action '{action}' over scope '{scope}' or the scope is invalid.

    (verify wording)

Any read or resolver error returns 500. It is never treated as "no assignments" (fail closed).

Matching:

- case-insensitive, where `*` matches any run of characters including `/`;
- it is a custom matcher of about 15 lines, because `path.Match` stops at `/`.

Out of scope: dataActions, deny assignments, ABAC conditions, groups, PIM, and data-plane enforcement.

**Tokens:** one `internal/token` package replaces both builders.

```go
type Claims struct{ OID, Sub, TID, AppID, Aud string /* ... */ }

func Issue(c Claims) string                 // alg none, as today
func Parse(bearer string) (Claims, bool)    // no signature check
```

- Client tokens: `oid` = `uuid.NewSHA1(tenantUUID, client_id)`. The `google/uuid` package is already a dependency. The default client `miniblue` keeps `...0002`, so existing users see no change.
- MI tokens: `oid` = principalId.
- CHANGELOG: non-default client ids now get distinct, stable `oid`s.

**Legacy guard (optional, not on any gate's critical path).** `authorization.Guard(a arm.Authorizer, ad arm.Admitter) func(http.Handler) http.Handler` applies the same checks to legacy chi ARM routes. It derives the action from the method and from `arm.ParseID(path)`. Build it only if users ask for enforcement on unmigrated types. Until then, **enforcement covers engine types only**, and the user docs say so. That includes resources created through deployments: the in-process `Dispatcher.Put` carries `ctx` (and so the principal) into `Engine.Do`. The HTTP fallback forwards the original `Authorization` header, so a Guard, if built later, sees the same principal.

### Policy

**`Microsoft.Authorization/policyAssignments`** becomes an engine type at MG, subscription and RG scope. Validate checks that `policyDefinitionId` resolves (400 `PolicyDefinitionNotFound`, verify).

**Unsupported rules fail closed, at configuration time.** A silent "doesn't match" is a false allow, which is the worst outcome for a test tool. `policy.Compile(def, params) (Rule, error)` turns a definition plus assignment parameters into an evaluable rule. It returns an error for any construct outside the subset below: unknown aliases, operators, functions, `value`, `count`, and the `denyAction` effect.

- **Enforcement on:** policyAssignment Validate calls `Compile`. An unsupported rule gives 400 `MinibluePolicyUnsupported`, naming the construct. The code is deliberately emulator-specific, so nobody mistakes it for Azure behaviour.
- **Evaluation** compiles again, because the definition may have changed since assignment. A compile failure there gives the same 400. It never allows.
- **Enforcement off:** assignments are CRUD only and `Compile` is not called, so any definition can be assigned.

**Enforcement** is off by default. `MINIBLUE_POLICY=enforce` wires `policy.Admitter`, which runs on PUT and PATCH proposed docs (`r.Doc`):

1. Collect assignments at the resource's scope and its ancestors. Skip any whose `notScopes` prefix the resource id.
2. Compile, then evaluate `policyRule.if` against `r.Doc`.
3. Resolve the effect, which may be `[parameters('effect')]`:
    - `deny`: 403 `RequestDisallowedByPolicy`, with the assignment and definition ids in `details`. With `enforcementMode: DoNotEnforce`, deny becomes audit.
    - `audit`: log one line.
    - `disabled`: nothing.
    - `modify`, `append`, `deployIfNotExists`, `auditIfNotExists` and `manual` never block a request in Azure either. They are treated as disabled and logged once per definition.

**Language subset:**

| Construct | Supported |
|---|---|
| Logical | `allOf`, `anyOf`, `not` |
| Fields | `type`, `name`, `location`, `kind`, `tags`, `tags['k']`, `tags.k`, `identity.type`, `<type>/sku.name`, `<type>/sku.tier` |
| Operators | `equals`, `notEquals`, `in`, `notIn`, `like`, `notLike`, `contains`, `notContains`, `exists` (case-insensitive) |
| Functions | `[parameters('x')]` only |

- Anything outside this table is a `Compile` error (see above). Nothing evaluates as "missing" or "no match" by accident.
- `value`, `count` and policy set definitions are deferred.

Package layout:

- `internal/services/policy/eval.go` (about 200 lines, `Compile` plus evaluation), with a table-driven test that includes the unsupported cases.

## Consequences

- Users can test least-privilege and deny-policy behaviour locally, opt-in, with zero cost when off. A nil Authorizer or Admitter means allow.
- Distinct `oid`s make multi-principal tests possible.
- Enforcement covers engine types only. Coverage grows as types migrate, and the optional Guard covers legacy types if users ask for it.
- Policy users whose definitions use constructs outside the subset get a clear 400 when enforcement is on, never a silent pass.

## Rejected alternatives

- **RBAC on by default:** it breaks every existing user whose client id is not the default.
- **Full ARM action catalogue** (an operations list per RP): large and unmaintainable. Derived `{type}/read|write|delete` actions are what built-in roles mostly use.
- **Real token signing and validation:** real authn is a non-goal.
- **Heuristic alias mapping** (`<type>/x` to `properties.x`): right often enough to mislead.
- **Unsupported policy input evaluates as "no match"** (fail open): a user would believe a deny policy works when it never fires.
- **Making the legacy Guard a gate prerequisite:** it doubles the enforcement surface for types that are about to migrate anyway.
- **`modify`, `append` and `deployIfNotExists`:** they mutate requests or create resources. Explicitly out of scope.
- **Policy evaluation on GET** (compliance state): not useful without the remediation machinery.
