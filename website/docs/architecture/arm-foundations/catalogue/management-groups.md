# Management Groups

> Engine fit: hooks plus engine-gap for `managementGroups` (typed errors, GET and DELETE hooks, a hierarchy index, cross-resource effects), passthrough for `settings`, bespoke for `subscriptions`, `descendants`, `getEntities` and `checkNameAvailability`. miniblue today: none (no `Microsoft.Management` registration, no root group). `internal/arm` already parses management group ids, and `internal/services/policy` registers `policyDefinitions` at subscription scope only. Suggested wave: new type after G2 ([SPIKE](../SPIKE.md) rank 3); the policy scope-mask change can ship earlier on its own.

Sync-only is the rule ([ADR-0006](../adr/0006-sync-lro-and-errors.md)), and real management group `PUT` and `DELETE` are long-running. The chosen miniblue behaviour is stated under [Pitfalls](#pitfalls) and flagged in the affected rows.

In the tables below, `MG` is `/providers/Microsoft.Management/managementGroups`.

## Types

Latest stable api-version is 2023-04-01 (confirmed in the REST spec repo). Older stable versions are 2019-11-01, 2020-02-01, 2020-05-01, 2020-10-01 and 2021-04-01; the latest preview is 2024-02-01-preview. azurerm and azapi pins were not checked (verify). `APIVersions` is metadata only (ADR-0005), so any well-formed version is accepted.

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.Management/managementGroups` | 2023-04-01 | tenant (`Scopes: arm.ScopeTenant`) | subscriptions, settings | `GET {g}/descendants`; `GET {g}` with `$expand` (children, path, ancestors), `$recurse`, `$filter` | hooks, engine-gap | `azerr.Error` typed errors; GET hook (computed `details.parent`, `$expand`); DELETE hook (refuse when children exist); `PatchMode` merge with flat body; `Request.Current`; hierarchy index over the store (children, recursive descendants); root bootstrap | H | M |
| `managementGroups/subscriptions` (association) | 2023-04-01 | child of managementGroups | none | `GET {g}/subscriptions` (list, later) | bespoke | cross-resource effect: a PUT moves the subscription out of any other group; computed `properties` from the subscriptions service | H | M |
| `managementGroups/settings` (singleton `default`) | 2023-04-01 | child of managementGroups | none | `GET {g}/settings` list | passthrough | `Hooks.Validate` (name must be `default`) | L | S |
| `MG/{g}/descendants` | 2023-04-01 | read-only | none | `GET` with `$top`, `$skiptoken` | bespoke (chi route beside the engine) | hierarchy index | M | S |
| `POST /providers/Microsoft.Management/getEntities` | 2023-04-01 | provider level | none | `POST`; `$search`, `$filter`, `$view`, `$select`, `$top`, `$skiptoken`, `groupName` | bespoke (provider-level chi route, allowed by ADR-0001) | hierarchy index | M | S |
| `POST /providers/Microsoft.Management/checkNameAvailability` | 2023-04-01 | provider level | none | `POST` body `{name, type}` | bespoke (provider-level, ADR-0001) | name rule shared with `Validate` (`CheckName`, ADR-0003) | L | S |
| `Microsoft.Authorization/policyDefinitions` at MG scope | 2023-04-01 (policy) | adds MG (and tenant for built-ins) | none | none | passthrough | scope mask only: add `arm.ScopeManagementGroup` to `Scopes` | M | S |
| `Microsoft.Authorization/roleAssignments` at MG scope | per [Slice 1](../slices/01-role-assignments.md) | includes MG | none | none | passthrough | Slice 1 scope mask; transitive ancestry only if RBAC is enforced at MG scope | M | S |
| `Microsoft.Resources/deployments` at MG scope | per [deployments](deployments.md) | MG | none | `validate`, `cancel`, `whatIf` | bespoke (deployments owner) | owned by the deployments page | M | M |
| `operations`, `startTenantBackfill`, `tenantBackfillStatus` | 2023-04-01 | provider level | none | static list; backfill pair | bespoke | no scenario in the source; static responses only if a client asks | L | S |

Phasing inside the type (M overall, L including the engine primitives):

1. Group CRUD, list, `$expand=children`, the name rule, the root group. Effort M.
2. Subscription association with move semantics, `descendants`, root auto-placement, `getEntities`. Effort M.
3. Settings, `checkNameAvailability`, the policy scope mask. Effort S.
4. Later: `$expand=path|ancestors`, `$recurse`, `$filter`, `GET {g}/subscriptions`, a cycle check on reparent (the cycle check should land in phase 1 if reparent ships there).

## miniblue gaps

Ordered by value.

- No management group service exists. `Microsoft.Management` is unregistered, so every `MG/...` path 404s. `ROADMAP.md:22` lists it as "Medium: hierarchy, subscription assignment".
- No root group and no hierarchy. The tenant id used by the subscriptions service is `00000000-0000-0000-0000-000000000001` (`internal/services/subscriptions/handler.go:56-57`), so the root group is named by that id with display name "Tenant Root Group".
- The subscriptions service serves one static subscription (`internal/services/subscriptions/handler.go:83-92`) and has no creation callback. Root auto-placement is cheapest derived at read time: a subscription with no association record is a child of the root group. That avoids a cross-service hook.
- `arm.ID` parses management group ids and classifies MG-scoped resources as `ScopeManagementGroup` (`internal/arm/id.go`, covered by `internal/arm/id_test.go`). The `Parent()` of a management group is the tenant, not the parent group, so any ancestry walk (RBAC, policy inheritance) needs a resolver over the hierarchy.
- `policyDefinitions` is registered at subscription scope only (`internal/services/policy/policy.go:13`). `TestPolicyDefinitionOnlyAtSubscriptionScope` (`tests/arm_policydefinition_test.go:78`) pins the MG-scope `PUT` to 404 today, so the scope-mask change must flip that assertion.
- The engine already does CRUD at MG scope for extension resources: `TestEngineCRUDAtEveryScope` (`internal/arm/engine_test.go:82-86`) puts a resource under a group that was never created. Per ADR-0003, extension resources at MG scope are not existence-checked, so writes under a missing group succeed until the MG type supplies an existence check.
- No `getEntities`, `descendants`, `checkNameAvailability` or `settings` routes.

## Scenarios

Status codes follow ADR-0006 (sync): `PUT` 201 on create and 200 on replace, `DELETE` 200 if it existed and 204 if missing, `PATCH` 200. Tests that also run against real Azure must accept the LRO forms (see Pitfalls).

### managementGroups

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T1 | Creating a group with an empty body works. The display name defaults to the group name. | `PUT MG/{g}` body `{}` => 201; `id` is `MG/{g}`, `type` is `Microsoft.Management/managementGroups`, `name` is `{g}`, `properties.displayName` equals `{g}`, `properties.tenantId` is the tenant id | T2 | Flag: real `PUT` is an LRO (200 or 202), miniblue is sync (ADR-0006), so create returns 201 and tests accept 200 or 201. Default parent is the root group (verify). Candidate `azurerm_management_group` fixture (T3, verify). |
| G2-T2 | Creating a group with a display name and a parent. | `PUT MG/{g}` body `properties.displayName` and `properties.details.parent.id` => 201; response `properties.details.parent.id` echoes the parent, with the parent's `name` and `displayName` filled in (verify) | T2 | Parent id is a full id, not a bare name. Same LRO flag as G2-T1. |
| G2-T3 | Putting an existing group again replaces its properties. | `PUT MG/{g}` again => 200; `displayName` is the new value | T2 | PUT replaces, PATCH merges (G2-T9). Whether an omitted `displayName` resets to the name and an omitted parent resets to the root is (verify). |
| G2-T4 | A parent that does not exist is rejected on create. | `PUT MG/{g}` with unknown `details.parent.id` => 400 `ParentManagementGroupNotFound` (verify) | T2 | Code and status (400 vs 404) are not confirmed against real Azure. |
| G2-T5 | A plain read does not return children. | `GET MG/{g}` => 200; `properties.children` absent; `properties.details.parent` present | T2 | |
| G2-T6 | `$expand=children` returns the direct children, groups and subscriptions. | `GET MG/{g}?$expand=children` => 200; `properties.children[]` items have `type`, `id`, `name`, `displayName`; group children nest their own `children` only when `$recurse=true` | T2 | Subscription child `type` string is `/subscriptions` in the spec text, but one spec example uses `Microsoft.Management/managementGroups/subscriptions` (verify). `$expand=path`, `$expand=ancestors` and `$recurse` can follow later. |
| G2-T7 | Reading a missing group. | `GET MG/{g}` => 404 `ManagementGroupNotFound` (verify) | T2 | Real Azure often answers 403 when the caller lacks read on a scope that does not exist (verify). Without RBAC enforcement, 404 is the useful behaviour. |
| G2-T8 | The list includes the root group and every created group. | `GET MG` => 200 `{value:[...]}`; items have `id`, `type`, `name`, `properties{tenantId, displayName}`; contains the root and each created group | T2 | List accepts `$skiptoken`; emit `nextLink` only if paging is implemented. Real list is limited to groups the caller can read; miniblue returns all. |
| G2-T9 | Renaming a group by `PATCH`. | `PATCH MG/{g}` body `{"displayName":"x"}` => 200; `properties.displayName` is `x`; parent and other fields unchanged | T2 | Body is flat (`displayName`, `parentGroupId`), no `properties` wrapper, per the spec. `PatchMode` merge. |
| G2-T10 | Reparenting by `PATCH`. | `PATCH MG/{g}` body `{"parentGroupId":"<full id>"}` => 200; `details.parent.id` is the new parent; `descendants` of the old parent no longer contain `{g}` and the new parent's do | T2 | `parentGroupId` is a full id. Reparenting under the group's own descendant must be rejected (code verify); see Pitfalls. |
| G2-T11 | Reparenting to an unknown parent. | `PATCH MG/{g}` with unknown `parentGroupId` => 400 `ParentManagementGroupNotFound` (verify) | T2 | Same code as G2-T4 (verify). |
| G2-T12 | Deleting a leaf group removes it. | `DELETE MG/{g}` => 200 (existed); then `GET MG/{g}` => 404 | T2 | Flag: real `DELETE` is an LRO (202 with a `Location` header, or 204). miniblue is sync with no async headers. Tests accept 200, 202 or 204 on the delete call. |
| G2-T13 | Deleting a group that does not exist. | `DELETE MG/{g}` => 204 | T2 | The spec lists only 202 and 204 for delete, so a 404 is not in the documented set (verify). ADR-0006 makes delete of a missing resource 204. |
| G2-T14 | A group that still has children cannot be deleted. | `DELETE MG/{g}` with a child group => 4xx `ManagementGroupHasChildren` (verify), group still present | T2 | Status (400 vs 409) and code (verify). Learn says the group must contain no child groups and no subscriptions (verify), so subscription children count too. Needs the DELETE hook plus a child lookup by exact parent id. |

### managementGroups/subscriptions

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T15 | Associating a subscription with a group. | `PUT MG/{g}/subscriptions/{s}` with no body => 200; association object has `name` `{s}`, `type` `Microsoft.Management/managementGroups/subscriptions`, `id` `MG/{g}/subscriptions/{s}`, `properties{displayName, parent{id}, state:"Active", tenant}` | T2/T3 | The spec declares no response body for this `PUT`; clients must not depend on it (verify). `displayName` comes from the subscriptions service. Candidate `azurerm_management_group_subscription_association` fixture (T3, verify). |
| G2-T16 | Associating into a group that does not exist. | `PUT MG/{g}/subscriptions/{s}` with missing `{g}` => 404 `ManagementGroupNotFound` (verify) | T2 | Child-type parent check ([ADR-0003](../adr/0003-pipeline-phases-and-hooks.md)) would give `ParentResourceNotFound` for a registered child; decide which code to assert. The service-specific code is (verify). |
| G2-T17 | A subscription belongs to one group. Associating it with group B after group A moves it. | `PUT MG/B/subscriptions/{s}` after `PUT MG/A/subscriptions/{s}` => 200; `GET MG/A/subscriptions/{s}` => 404; `GET MG/B/subscriptions/{s}` => 200; `MG/A?$expand=children` no longer lists it | T2 | Cross-resource effect, so this is the reason the type is bespoke. |
| G2-T18 | Reading an association that does not exist. | `GET MG/{g}/subscriptions/{s}` for a non-member => 404 `SubscriptionNotFound` (verify) | T2 | Message names both the subscription and the group (verify). |
| G2-T19 | Removing an association. | `DELETE MG/{g}/subscriptions/{s}` => 200; then `GET` => 404 `SubscriptionNotFound` (verify) | T2 | Spec lists 200 and 204 for this delete, no LRO. Real Azure re-homes the subscription under the root or default group (verify); miniblue derives that from "no association means root". |

### descendants

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T20 | `descendants` lists nested groups and subscriptions recursively. | `GET MG/{g}/descendants` => 200 `{value:[...]}`; group items have `id`, `type` `Microsoft.Management/managementGroups`, `name`, `properties{displayName, parent.id}`; subscription items have `id` `/subscriptions/{s}` and `properties.parent.id` set to the group id | T2 | Subscription item `type` is (verify): spec text says `/subscriptions`, spec example shows `Microsoft.Management/managementGroups/subscriptions`. Items carry `nextLink: null`. Supports `$top` and `$skiptoken`. |
| G2-T21 | Empty and missing groups. | `GET MG/{g}/descendants` => 200 `{value:[]}` for an empty group; 404 `ManagementGroupNotFound` (verify) for a missing one | T2 | |

### getEntities

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T22 | `getEntities` lists every group and subscription in the tenant with counts. | `POST /providers/Microsoft.Management/getEntities` => 200; entities have `id`, `type`, `name`, `properties{tenantId, displayName, parent.id, permissions, inheritedPermissions, numberOfDescendants, numberOfChildren, numberOfChildGroups, parentDisplayNameChain, parentNameChain}` | T2 | Real verb is **POST**. A `GET` alias is an optional miniblue extension, not real behaviour. Query params: `$search`, `$filter`, `$view`, `$select`, `$top`, `$skiptoken`, `groupName`, header `Cache-Control`. `permissions` is an enum (noaccess, view, edit, delete in the spec, verify values); returning `edit` is fine without RBAC. |
| G2-T23 | A subscription entity has no child counts. | `POST getEntities` => subscription entity with `type` `/subscriptions` (verify), `id` `/subscriptions/{s}`, `parent.id` set to its group, no `numberOfChild*` fields | T2 | |

### settings

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T24 | Hierarchy settings are a singleton named `default`. | `PUT MG/{g}/settings/default` => 200; `GET` returns `properties{tenantId, requireAuthorizationForGroupCreation, defaultManagementGroup}` | T2 | Real settings apply to the tenant root group (verify whether other groups are rejected). `PUT` replaces and `PATCH` merges; a merging `PUT` would be wrong. `Validate` rejects any name except `default`. |
| G2-T25 | `PATCH` on settings that were never created. | `PATCH MG/{g}/settings/default` with no existing settings => 404 `HierarchySettingsNotFound` (verify) | T2 | |
| G2-T26 | Settings list and delete. | `GET MG/{g}/settings` => 200 with 0 or 1 items; `DELETE MG/{g}/settings/default` => 200 and the list is empty after; missing group => 404 `ManagementGroupNotFound` (verify) | T2 | Spec lists 200 for settings `DELETE`. |

### Root group

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T27 | The tenant has a root group named by the tenant id, and new subscriptions land in it. | after startup `GET MG/00000000-0000-0000-0000-000000000001` => 200, `displayName` "Tenant Root Group", no `details.parent`; a subscription created later appears in `MG/{root}/descendants` with no explicit association | T2 | Real root group cannot be deleted (verify). `GET MG` lists it (G2-T8). Needs a startup seed; placement derived at read time (see miniblue gaps). |

### Extension resources at MG scope

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T28 | Role assignments can be written at MG scope. | `PUT MG/{g}/providers/Microsoft.Authorization/roleAssignments/{n}` => 201, then `GET` => 200, then `PUT` again => 200 | T2 | Depends on the roleAssignments engine type ([Slice 1](../slices/01-role-assignments.md)) including MG in its scope mask (verify). Per ADR-0003 an extension resource at MG scope is not existence-checked, so a write under a missing group succeeds; add a check once the MG type exists. An RBAC enforcer that walks `Parent()` skips MG ancestry (see miniblue gaps). |
| G2-T32 | `policyDefinitions` work at MG scope. | `PUT MG/{g}/providers/Microsoft.Authorization/policyDefinitions/{n}` => 201, `GET` => 200, `GET .../policyDefinitions` => 200 `{value:[...]}` | T2 | One-line change: add `arm.ScopeManagementGroup` to the `Scopes` mask (`internal/services/policy/policy.go:13`), plus `arm.ScopeTenant` if built-ins are served at tenant scope. Flip `tests/arm_policydefinition_test.go:78`. Assignments and initiatives are not registered. miniblue does not evaluate policy, so the hierarchy only needs to be listable. Same missing existence check as G2-T28. |
| G2-T33 | Deployments work at MG scope. | `PUT MG/{g}/providers/Microsoft.Resources/deployments/{n}`, `GET`, list, `validate`, `cancel` follow the deployments rules | T2 | Cross-owner: belongs to the page that owns deployments ([deployments](deployments.md), X40 in [cross-cutting](cross-cutting.md)). Needs the deployments service to dispatch at tenant and MG scope. MG-scope deployments carry a `location` in the body (verify). Not implemented on this page. |

### Names and name checks

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T29 | Group names follow the documented rule: 1 to 90 characters of alphanumerics, hyphens, underscores, periods and parentheses, starting with a letter or number, not ending in a period. | `PUT MG/{g}` with a 91-character name, a leading `-`, a trailing `.`, a space, or `/` => 400; `a-b_c.(d)` => 201 | T2 | `new` (real API only). Implement in `Hooks.Validate` with `CheckName` (ADR-0003); error code is (verify). Blocking only path-traversal characters is not enough. Unit-test the regexp in `internal/arm`. |
| G2-T30 | `checkNameAvailability` reports whether a name can be used. | `POST /providers/Microsoft.Management/checkNameAvailability` body `{name, type:"Microsoft.Management/managementGroups"}` => 200 `{nameAvailable, reason, message}`; `reason` is `Invalid` for a rule violation and `AlreadyExists` for a name in use | T2 | `new` (spec 2023-04-01). Provider-level chi route (ADR-0001). Reuse the G2-T29 rule. A wrong `type` is `Invalid` (verify). |

## Pitfalls

- Real `PUT` (200 or 202, `status: NotStarted` in the 202 body) and `DELETE` (202 with a `Location` header, or 204) on a group are long-running. ADR-0006 is sync-only, so miniblue returns 201 or 200 on `PUT` and 200 or 204 on `DELETE`, with no async headers. SDK pollers and `az` accept a synchronous terminal response. A suite that also runs against real Azure must accept 200, 201, 202 and 204 and poll the `Location` header.
- `getEntities` is `POST` in real Azure. Asserting `GET` would be wrong.
- The `PATCH` body is flat (`displayName`, `parentGroupId`). Wrapping it in `properties` is wrong. `PUT` uses `properties.displayName` and `properties.details.parent.id`.
- "Has children" is an exact match on the stored parent id. A prefix or suffix match wrongly counts `mg1-eu` or `mg10` as children of `mg1`.
- Reparenting needs a cycle check (a group cannot move under its own descendant).
- Ids are case-insensitive. Look up by the normalised id and return canonical casing; two groups differing only in case are one group.
- Validating only path-traversal characters in names accepts names that real Azure rejects (the rule in G2-T29).
- The root group id is the tenant id, `GET MG` includes it, and it cannot be deleted or reparented (verify).
- A subscription belongs to exactly one group. Association `PUT` is a move, not an add. Removing the association re-homes the subscription (verify).
- Real Azure rejects an association for a subscription that does not exist (verify). miniblue's subscriptions service echoes any id today (`internal/services/subscriptions/handler.go:83-92`), so a check would either always pass or reject all but the static subscription. Decide the behaviour and note it in the PR.
- Settings `PUT` replaces and `PATCH` merges; settings `DELETE` returns 200 (not 204).
- Extension resources at MG scope are not existence-checked (ADR-0003). Role, policy and deployment writes under a non-existent group succeed until a group existence check is added.
- RBAC at MG scope needs a transitive ancestry walk over the hierarchy. The `Parent()` of a management group id is the tenant, not its parent group.
- Policy at MG scope is only a scope-mask change because miniblue does not evaluate policy. If evaluation is ever added, inheritance down the hierarchy becomes part of the work.

## Dropped

| ID | Reason |
|---|---|
| G2-T31 | Duplicate: the id round trip for `/providers/Microsoft.Management/managementGroups/{g}` is already covered by `internal/arm/id_test.go` (the "management group" case). |
