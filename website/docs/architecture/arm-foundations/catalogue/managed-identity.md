# Managed Identity

> Engine fit: hooks for `userAssignedIdentities`; passthrough child plus `Validate` for `federatedIdentityCredentials`; bespoke for `identities/default`. miniblue today: hand-written service `internal/services/identity` (user-assigned identities by RG only: PUT, GET, PATCH tags, DELETE, list by RG). Suggested wave: wave 2 for the user-assigned identity ARM type (PLAN phase 4); `federatedIdentityCredentials` as a new type after G2 (cheap-win bundle).

Spec facts used below are confirmed against the `msi` stable spec 2024-11-30 (earlier stable versions 2023-01-31 and 2018-11-30). `tenantId`, `principalId` and `clientId` are read-only. `properties.isolationScope` is `None` or `Regional`. Federated credential names match `^[a-zA-Z0-9]{1}[a-zA-Z0-9-_]{2,119}$`; `issuer` (a URI), `subject` and `audiences` are required.

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.ManagedIdentity/userAssignedIdentities` | 2024-11-30, 2023-01-31, 2018-11-30 | RG; list by subscription | `federatedIdentityCredentials` | `POST listAssociatedResources` `(verify)` | hooks | `Defaults` with `Request.Current` (stable `principalId`, `clientId`, `tenantId`), `PatchMode` merge (PATCH is a full update in the spec), `ListSubscription` (PLAN 1.2), RG existence | L | S |
| `userAssignedIdentities/federatedIdentityCredentials` | 2024-11-30, 2023-01-31 | child of identities | none | none | passthrough child + `Validate` | parent existence (`ParentResourceNotFound`), `Validate` (name pattern, required `issuer`, `subject`, `audiences`) | H | S |
| `{scope}/providers/Microsoft.ManagedIdentity/identities/default` | 2018-11-30 | any resource scope | none | `GET` only | bespoke | reads the `identity` block of the resource at `{scope}`; no engine seam for a cross-type read `(verify)` | M | S |

## miniblue gaps

Ordered by value.

- **No `federatedIdentityCredentials`.** No routes, store or tests. This is the cheap high-value addition (workload identity setups in Terraform use it).
- **No list by subscription** (`internal/services/identity/handler.go`, list is by RG only). Depends on PLAN 1.2.
- **No `identities/default`.** It also needs other services to expose an `identity` block on their resources.
- DELETE answers 204 on success and 404 on a missing identity (`handler.go:173-185`). Real Azure answers 200 when the identity existed and 204 when it did not, and never 404 `(verify)`.
- PUT does its RG check through the legacy `rg:` key (`handler.go:73`) and returns 404 `ResourceGroupNotFound`; the engine check supersedes it.
- `principalId` and `clientId` are derived deterministically from the resource id (`deterministicUUID`, `handler.go:99-100`), and `tenantId` is a constant (`00000000-0000-0000-0000-000000000001`). Derivation keeps them stable across PUT, but delete and recreate yields the same ids; real Azure issues new ones.
- PATCH updates tags only (`handler.go:126`); `isolationScope` is not modelled. PUT replaces tags wholesale.
- Existing coverage: `internal/services/identity/handler_test.go` (`TestUpdateIdentityDoesNotMutatePublishedRecord`, `TestUserAssignedIdentityLifecycleAndPatchTags`) and the azurerm fixture `tests/terraform/azurerm/identity-rbac/main.tf` (an identity plus role assignments). No federated credential fixture.

## Scenarios

### userAssignedIdentities

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T109 | Create an identity. | `PUT userAssignedIdentities/{n}` => 201; `properties.principalId`, `clientId` and `tenantId` are GUIDs; `location` and `type` as sent. | T2 | Already covered by the existing handler tests; port to an `armtest` `Sample` row. Assert GUID shape only, not the tenant value. |
| G2-T110 | Create in a missing resource group. | `PUT` => 404 `ResourceGroupNotFound`. | T1 | Engine RG existence check. Already covered. |
| G2-T111 | Repeat the same PUT. | Second `PUT` => 200 (not 201); `principalId` and `clientId` unchanged. | T1 | |
| G2-T112 | Tags at create and on PATCH. | `PUT` with tags => 201 and tags stored; `PATCH` `{tags}` => 200 and tags updated; other properties unchanged. | T1 | Whether PATCH replaces or merges the tag set is `(verify)`; `PatchMerge` merges key by key. |
| G2-T113 | List by RG and by subscription. | `GET .../resourceGroups/{rg}/providers/Microsoft.ManagedIdentity/userAssignedIdentities` => 200 with only that group's identities; `GET /subscriptions/{s}/providers/Microsoft.ManagedIdentity/userAssignedIdentities` => 200 with all. | T1 | Subscription list is new; needs `ListSubscription` (PLAN 1.2). |
| G2-T114 | Two identities are distinct. | `principalId` and `clientId` differ between identities. | T2 | Derive from the resource id (distinct inputs, distinct outputs). |
| G2-T120 | azurerm and azapi create and destroy an identity; add a federated credential. | Apply and destroy succeed; tags preserved; `azurerm_federated_identity_credential` (new) applies, reads back `issuer`, `subject`, `audience`, and destroys. | T3 | Extend `tests/terraform/azurerm/identity-rbac/main.tf`; add an azapi variant. The federated credential part is `new` and needs the FIC scenarios first. |
| G2-N13 | `new`: delete an identity. | `DELETE` existing => 200 then `GET` => 404 `ResourceNotFound`; `DELETE` again => 204. | T1 | `new`. Today answers 204 then 404 (`handler.go:173-185`). |
| G2-N14 | `new`: isolation scope. | `PUT` with `properties.isolationScope` `Regional` => 201 and `GET` returns it; omitted behaves as `None` `(verify)`. | T2 | `new`. Spec enum confirmed (`None`, `Regional`); default is `(verify)`. |

### federatedIdentityCredentials

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T116 | Create, get, list, update, delete a credential. | `PUT .../federatedIdentityCredentials/{n}` => 201; update => 200; `GET` round-trips `properties.issuer`, `subject`, `audiences`; list shows it; `DELETE` => 200 then `GET` => 404. | T2 | Passthrough child. A body is required. Name pattern is enforced by `Validate`. |
| G2-T117 | Missing parent identity, missing credential. | `PUT` or `GET` of a credential under a missing identity => 404 (`ParentResourceNotFound` per the engine rule; the real provider may return `ResourceNotFound` `(verify)`); `GET` of a missing credential => 404 `ResourceNotFound`. | T1 | Service-specific not-found codes are not real ARM codes. |
| G2-T118 | PUT without `properties`, or missing a required field. | `PUT` => 400 `InvalidRequestContent` `(verify)`; the spec requires `issuer`, `subject` and `audiences`. | T2 | Error code per ADR-0006; the real provider's own code is `(verify)`. |

### identities/default

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T119 | Read the system-assigned identity of a resource. | `GET {scope}/providers/Microsoft.ManagedIdentity/identities/default` on a resource with no system identity => 404 `IdentityNotFound` `(verify)`; with one => 200, `location` present, `principalId` and `tenantId` matching the resource's `identity` block. | T2 bespoke | Bespoke: depends on other services' `identity` blocks. |

## Pitfalls

- `principalId` and `clientId` are stable for the life of the identity, and different after delete and recreate. A deterministic derivation from the resource id gets the first and not the second.
- Real DELETE is 200 when the resource existed and 204 when it did not. It never returns 404.
- The not-found code for a missing parent is `ParentResourceNotFound` in the engine and `ResourceNotFound` for the resource itself. A per-service code such as `ManagedIdentityNotFound` is not a real ARM code.
- PUT is 201 on create and 200 on replace.
- A missing identity named in another resource's `identity.userAssignedIdentities` is rejected or echoed by real Azure `(verify)`; decide once and test it where the consumer lives.
- Missing or malformed api-version is 400; any well-formed version is accepted.
- A federated credential's `audiences` is an array (azurerm exposes it as `audience`); `issuer` must be a valid HTTPS URI `(verify)`.

## Dropped

| ID | Reason |
|---|---|
| G2-T115 | Asserts a service principal exists in the directory after creation. Needs an Entra or Graph service; out of scope. |
