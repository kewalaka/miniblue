# Slice 1: Role assignments on the engine, with opt-in enforcement

Proves:

- `Request.Current` (stable `createdOn`, immutable fields);
- `Reader` cross-lookups (role definitions, duplicates);
- the `List` override;
- `PatchNone`;
- typed errors;
- extension resources at every scope;
- the `Authorizer` seam.

Depends on: PLAN phase 1 (`Engine.Do`, hooks, Reader, Principal, token package).

## Today

`internal/services/authorization/handler.go`:

- PUT (`:177`) rebuilds the doc from an allow-list. `createdOn` and `updatedOn` are `time.Now()` on every PUT (`:206`).
- No validation beyond "properties present".
- Key is `authorization:roleAssignment:<scope as sent>:<name>` (`:169`). It is case-sensitive, so a scope sent with different casing misses.
- List (`:231`) is exact-scope prefix only.
- Delete of a missing assignment returns 404. Real ARM returns 204.
- `principalType` is echoed, or `""` when absent (`:205`). Keep that.

## Registration

```go
// internal/services/authorization/types.go
func Register(reg *arm.Registry, h *Handler) {
    reg.Register(arm.Type{
        Name:        "Microsoft.Authorization/roleAssignments",
        APIVersions: []string{"2022-04-01"},
        Scopes:      arm.ScopeTenant | arm.ScopeManagementGroup | arm.ScopeSubscription | arm.ScopeResourceGroup | arm.ScopeResource,
        Patch:       arm.PatchNone,
        List:        h.listAssignments,
        Hooks:       arm.Hooks{Validate: h.validateAssignment, Defaults: defaultAssignment},
    })
}
```

`arm.ID` already parses extension resources on arbitrary resource scopes (`id.go:25,106`). Add an armtest case at a child-resource scope anyway.

A resource-scope assignment is **not** existence-checked against its scope (ADR-0003, phase 5), because the scope is usually a legacy resource the engine cannot see. RG and subscription scopes are checked through `ScopeExists`.

## Validate (under lock, sees Current)

| Rule | Error |
|---|---|
| name is a GUID | 400 `InvalidRoleAssignmentId` (verify) |
| `properties.roleDefinitionId` present and resolvable (built-in by trailing GUID, or custom at scope or an ancestor) | 400 `RoleDefinitionDoesNotExist` (verify) |
| `properties.principalId` present and a GUID | 400 `InvalidPrincipalId` (verify). There is no directory, so nothing beyond GUID shape. |
| `Current != nil` and principalId, roleDefinitionId or scope changed | 400 `RoleAssignmentUpdateNotPermitted` (verify) |
| Another assignment at the same scope with the same principalId and role GUID, under a different name | 409 `RoleAssignmentExists` |

Shared resolver, used by Validate and injected into the Enforcer as its `RoleResolver` (ADR-0008):

```go
// ResolveRoleDefinition finds a built-in by GUID, else a custom definition at scope or an ancestor.
func (h *Handler) ResolveRoleDefinition(id string, scope arm.ID) (map[string]any, bool, error)
```

Custom definitions stay at their legacy keys until roleDefinitions migrate (gate G3). The resolver reads those keys lock-free, through `Store.Lookup` and `Store.Scan`, so backend errors propagate as 500. The resolver is the only code that knows where definitions live. Migrating roleDefinitions later changes its body, not its callers.

## Defaults (idempotent)

- `properties.scope` = `r.ID.Scope.String()` (as sent).
- `createdOn` = `Current.properties.createdOn`, else now.
- `updatedOn` = now on create; on update, now only if the properties changed, else Current's value.
- Echo `principalType` (`""` when absent, as today), `condition`, `conditionVersion`, `delegatedManagedIdentityResourceId` and `description`.
- Drop all other input properties. This matches today's allow-list.

## List override

Collection GET at `{scope}/providers/Microsoft.Authorization/roleAssignments`:

| `$filter` | Result |
|---|---|
| none | at, above and below scope (verify against a real subscription before PR 1) |
| `atScope()` | at and above |
| `principalId eq '{id}'` | at, above and below, for that principal |

- Combine clauses by splitting on ` and ` (case-insensitive). An unknown clause gives 400 `InvalidFilter` (verify).
- `assignedTo('{id}')` is not documented for 2022-04-01, so it is not implemented. Add it only if a client is seen sending it.
- "Above" walks `scope.Parent()` with `Reader.Children`. "Below" uses `Reader.Under(scope, type)`.

## Persisted state

Bump `store.SchemaVersion`, with the CHANGELOG line `state reset required: role assignments moved to the ARM engine`. There is no data migration (ADR-0004 §5). The case-sensitive legacy keys go away with the reset.

## Removal

In `handler.go`, delete:

- `handleRoleAssignments` and the create, get, list and delete functions;
- `roleAssignmentKey` and `roleAssignmentPrefix`;
- the `roleAssignments` lines in `registerScope`.

Narrow `Middleware` (`:45`) to `roleDefinitions` paths only, and let everything else fall through. The overlap guard fails boot if any chi roleAssignments route is left.

## Enforcement (`MINIBLUE_RBAC=enforce`)

`Enforcer` as specified in ADR-0008, wired with `arm.WithAuthorizer` when the env var is set.

In the same PR as the Enforcer (or just before it):

- fix Contributor `notActions`;
- add User Access Administrator.

Tests (in-process, `internal/services/authorization`):

| Test | Expect |
|---|---|
| Enforcement off (default), non-default principal | Every write is allowed |
| On, default principal (`...0002`) | Allowed (implicit admin) |
| On, principal with no assignments | 403 `AuthorizationFailed` on PUT. The message names the action and scope. |
| Reader at subscription | GET in RG allowed; PUT in RG denied (inheritance) |
| Contributor at RG | PUT resource allowed; PUT roleAssignment denied (`notActions`) |
| Custom role `Microsoft.ApiManagement/service/read` | Read allowed, write denied |
| Assignment at resource scope | Applies to that resource and its children only |
| Mixed-case scope in assignment vs request | Still matches |
| Custom role definition stored at its legacy key | Resolved through `RoleResolver`; permissions apply |
| Store read fails during the walk | 500, not 403 and not allow |

Tokens come from `internal/token.Issue` with distinct client ids, so tests also cover the per-client `oid`.

## Acceptance

- The armtest sample for roleAssignments passes. `Update` changes only `description`. `Invalid` uses a non-GUID principalId.
- Existing `authorization/handler_test.go` and `tests/` stay green. The listed intentional changes:
    - delete-missing returns 204;
    - list includes above and below scope;
    - `createdOn` is stable.
- Optional local run: `azurerm_role_assignment` apply, then a second plan is empty, then destroy.

## PRs

1. `feat(authorization): serve roleAssignments via arm engine` (registration, hooks, list, schema bump, removal)
2. `fix(authorization): contributor notActions; add User Access Administrator`
3. `feat(authorization): opt-in RBAC enforcement`
