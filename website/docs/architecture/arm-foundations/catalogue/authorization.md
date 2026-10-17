# Authorization

> Engine fit: hooks for roleAssignments; hooks plus engine gaps for roleDefinitions; bespoke for RBAC enforcement (per type below). miniblue today: hand-written service `internal/services/authorization` (bespoke `Dispatch` behind a middleware). Suggested wave: roleAssignments in Phase 2 slice 2.1 ([PLAN](../PLAN.md)), design in [slice 1](../slices/01-role-assignments.md); roleDefinitions at gate G3 ([ADR-0008](../adr/0008-rbac-and-policy-minimum.md)); enforcement at slice 2.3 (opt-in).

Real latest stable api-version is 2022-04-01 for both types (spec confirmed; some role-definition paths also appear in a 2022-05-01 preview). The validation, defaults, list override and removal design for roleAssignments lives in [slice 1](../slices/01-role-assignments.md); this page only lists the scenarios that slice must satisfy and does not repeat its rules.

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.Authorization/roleAssignments` | 2022-04-01 | management group, subscription, RG, resource, nested resource (extension resource at every scope) | none | list at scope with `$filter` (`atScope()`, `principalId eq`); `GET` and `DELETE` by full id | hooks (+ duplicate detection in `Validate`) | `Request.Current`, `Hooks{Validate, Defaults}`, `PatchMode` `PatchNone`, list override, `Reader` cross-lookups; see [slice 1](../slices/01-role-assignments.md) | H | S |
| `Microsoft.Authorization/roleDefinitions` | 2022-04-01 | management group, subscription, RG, resource; tenant for `GET` by id | none | `$filter`; paged list | hooks + engine gaps | list override and paging, built-in seed data, `Get`-fallback seam for built-ins (decision at gate G3, [ADR-0008](../adr/0008-rbac-and-policy-minimum.md)) | H | S |
| RBAC enforcement on ARM calls | n/a | all | none | none | bespoke (`Authorizer` seam) | `arm.Authorizer`, `MINIBLUE_RBAC=enforce` ([ADR-0008](../adr/0008-rbac-and-policy-minimum.md)) | L | L |

Spec also lists `permissions`, deny assignments and `providerOperations`. They have no scenarios in this catalogue and are out of scope ([ADR-0008](../adr/0008-rbac-and-policy-minimum.md): no deny assignments, dataActions or ABAC).

## miniblue gaps

Ordered by value. Bugs carry `file:line` in `internal/services/authorization/handler.go`.

- Role assignment PUT regenerates `createdOn` and `updatedOn` on every call (`:206-207`), and there is no validation beyond "properties present". GUID name, `principalId`, `roleDefinitionId` resolution, immutable fields and duplicates are all unchecked; the fix is [slice 1](../slices/01-role-assignments.md).
- No duplicate detection: a second assignment for the same principal, role and scope is accepted.
- The storage key is `authorization:roleAssignment:<scope as sent>:<name>` (`:169`), so it is case-sensitive.
- DELETE of a missing assignment returns 404; real ARM returns 204. DELETE of an existing one returns 200 with the body, which matches the spec (200 or 204).
- Contributor is seeded with `actions ["*"]` and empty `notActions` (`:31`, `:369`), so it equals Owner. [ADR-0008](../adr/0008-rbac-and-policy-minimum.md) specifies the correct `notActions` and adds User Access Administrator.
- Tenant-level `GET /providers/Microsoft.Authorization/roleDefinitions/{id}` is rejected: `parseAuthorizationPath` returns not-ok when the marker is at index 0 (`:102-104`). Only 9 built-ins are seeded (`:29-38`).
- Role definition list handles only `$filter=roleName eq '...'` (`:316-319`); no `atScopeAndBelow`, no `type eq` filter and no paging.
- Role definition PUT on a built-in id returns 400 and custom DELETE returns 204 or 404 (`:343`). Real DELETE of an existing definition returns 200 with the body (spec: 200 or 204).
- Management-group and nested-resource scopes are served by the generic path parse in the middleware (`:45-53`), but nothing in the tests exercises them `(verify)`.
- No enforcement of any kind; the opt-in design is in [ADR-0008](../adr/0008-rbac-and-policy-minimum.md).
- Tests: `internal/services/authorization/handler_test.go`, `internal/server/authorization_test.go`. Terraform: `tests/terraform/azurerm/identity-rbac` covers `azurerm_role_assignment`; no `azapi` fixture.

## Scenarios

### roleDefinitions

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T48 | Custom role lifecycle at subscription scope | `PUT /subscriptions/{s}/providers/Microsoft.Authorization/roleDefinitions/{guid}` => 201 (spec lists 201 only); `GET` => 200; update via `PUT` => 201 `(verify)`; `DELETE` => 200; `GET` after => 404 | T2 | 404 code `(verify)`; `RoleDefinitionNotFound` is not confirmed. Real role names must be unique and the id must be a GUID `(verify)`. Spec lists 201 for update, so asserting 200 may be wrong. |
| G1-T49 | List at subscription scope includes built-ins | `GET .../roleDefinitions` => 200; contains Owner, Contributor and Reader with `properties.type` `BuiltInRole`; a custom role appears with `CustomRole`; each item has `id`, `name`, `roleName`, `permissions`, `assignableScopes` | T1/T2 | The 2022-04-01 property is `type`, not `roleType`. Needs the built-in seed. |
| G1-T50 | Filter and page the list | `GET .../roleDefinitions?$filter=roleName eq '{name}'` => 200 with exactly the matching definition; a large result returns `nextLink` | T2 | Needs list override and paging. Real paging is `nextLink`, not a `hasMore` flag. Page size `(verify)`. Spec also defines `atScopeAndBelow`. |
| G1-T51 | Get a built-in by id at tenant level | `GET /providers/Microsoft.Authorization/roleDefinitions/{guid}` => 200 with the built-in | T2 | Spec has the by-id form. Returns 404 today (see gaps). Needs the `Get`-fallback seam if the type moves. |

### roleAssignments

See [slice 1](../slices/01-role-assignments.md) for the hooks that implement these.

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T52 | Assignment lifecycle at subscription scope | `PUT .../roleAssignments/{guid}` body `{properties:{roleDefinitionId, principalId, principalType}}` => 201; `properties.scope` `/subscriptions/{s}`, `principalId`, `roleDefinitionId` echoed; `createdOn` stable across reads; `GET` => 200; `DELETE` => 200; `GET` after => 404 | T1/T2/T3 | Spec: PUT 200 or 201. 404 code `RoleAssignmentNotFound` `(verify)`. T3 is the existing `azurerm_role_assignment` fixture. |
| G1-T53 | Assignment at other scopes | `PUT` at management group, RG, resource and nested-resource scope => 201; `properties.scope` equals the path scope; `id` is `{scope}/providers/Microsoft.Authorization/roleAssignments/{guid}` | T1/T2 | A resource scope is not existence-checked ([slice 1](../slices/01-role-assignments.md)). Management-group path shape `(verify)`. |
| G1-T54 | List at scope | `GET {scope}/providers/Microsoft.Authorization/roleAssignments` => 200 `{value:[...]}` containing the created assignment; `$filter=atScope()` and `principalId eq '{id}'` narrow the result | T2 | Default scope semantics (at, above or below) `(verify)`, tracked in slice 1. |
| G1-T55 | Non-GUID assignment name | `PUT .../roleAssignments/not-a-guid` => 400 `InvalidRoleAssignmentId` | T1/T2 | Code `(verify)`. Maps to the `Invalid` armtest case. |
| G1-T56 | Duplicate assignment | A second `PUT` with the same principal, role and scope under a different name => 409 `RoleAssignmentExists` | T2 | Assert 409; 400 is wrong. Same name and same content is idempotent (200). |
| G1-T57 | Unknown role definition | `PUT` with a `roleDefinitionId` that resolves to nothing => 400 `RoleDefinitionDoesNotExist` | T2 | Code and status `(verify)`; 404 `RoleDefinitionNotFound` is not confirmed. Built-in by trailing GUID, or custom at the scope or an ancestor. |

### Enforcement (deferred)

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G1-T58 | RBAC scope inheritance on ARM calls | With `MINIBLUE_RBAC=enforce`: Reader at subscription => `GET` of a resource below it 200, `PUT` 403 `AuthorizationFailed`; an assignment at RG A grants nothing in sibling RG B (403); an assignment above applies to scopes below | T2 | Deferred to slice 2.3 ([ADR-0008](../adr/0008-rbac-and-policy-minimum.md)). Covers engine types only; data-plane enforcement and management-group propagation are out of scope until MG parents are modelled `(verify)`. |

## Pitfalls

- Real Azure returns 409 `RoleAssignmentExists` on a duplicate; asserting 400 `RoleAssignmentAlreadyExists` would be wrong.
- Assignment names must be GUIDs. Real Azure rejects other names with 400 on the ARM call itself, not only in a client tool.
- The 2022-04-01 role-definition property is `type` (`BuiltInRole` or `CustomRole`); there is no `roleType`.
- The role-definition PUT spec documents 201 only. A strict SDK client can reject a 200 on update.
- Paging uses `nextLink`; do not invent a `hasMore` field.
- A role definition is addressed by its GUID name; `roleName` is a property and is matched by `$filter`, not by the URL.
- DELETE of a missing role assignment is 204, not 404.
- Contributor must not be able to write or delete role assignments (`notActions` includes `Microsoft.Authorization/*/Write` and `*/Delete`); seeding it as `*` hides the difference between Contributor and Owner.
- Role assignments are extension resources: they can sit under any resource id, so the engine must accept arbitrary parent scopes without an existence check.

## Dropped

| ID | Reason |
|---|---|
| (none) | All source ids G1-T48 to G1-T58 are in the tables above; G1-T58 is a deferred row. |
