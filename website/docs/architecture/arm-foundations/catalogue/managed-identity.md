# Managed Identity

> Engine fit is hooks for `userAssignedIdentities`, passthrough child plus `Validate` for `federatedIdentityCredentials`, and bespoke for `identities/default`. miniblue today has a hand-written service `internal/services/identity` that supports user-assigned identities by resource group only (PUT, GET, PATCH tags, DELETE, and list by resource group). Suggested wave is wave 2 for the user-assigned identity ARM type (PLAN phase 4), and `federatedIdentityCredentials` as a new type after G2.

Spec facts used below are confirmed against the `msi` stable spec 2024-11-30 (earlier stable versions are 2023-01-31 and 2018-11-30). `tenantId`, `principalId` and `clientId` are read-only. `properties.isolationScope` is `None` or `Regional`. Federated credential names match `^[a-zA-Z0-9]{1}[a-zA-Z0-9-_]{2,119}$`; `issuer` (a URI), `subject` and `audiences` are required.

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.ManagedIdentity/userAssignedIdentities` | 2024-11-30, 2023-01-31, 2018-11-30 | RG; list by subscription | `federatedIdentityCredentials` | `POST listAssociatedResources` `(verify)` | hooks | `Defaults` with `Request.Current` (stable `principalId`, `clientId`, `tenantId`), `PatchMode` merge (PATCH is a full update in the spec), `ListSubscription` (PLAN 1.2), RG existence | L | S |
| `userAssignedIdentities/federatedIdentityCredentials` | 2024-11-30, 2023-01-31 | child of identities | none | none | passthrough child + `Validate` | parent existence (`ParentResourceNotFound`), `Validate` (name pattern, required `issuer`, `subject`, `audiences`) | H | S |
| `{scope}/providers/Microsoft.ManagedIdentity/identities/default` | 2018-11-30 | any resource scope | none | `GET` only | bespoke | reads the `identity` block of the resource at `{scope}`; no engine seam for a cross-type read `(verify)` | M | S |

## miniblue gaps

Ordered by value.

- `federatedIdentityCredentials` has no routes, store or tests. This is a cheap, high-value addition because workload identity setups in Terraform use it.
- List by subscription is missing (`internal/services/identity/handler.go` lists by resource group only). This depends on PLAN 1.2.
- `identities/default` is missing. It also needs other services to expose an `identity` block on their resources.
- DELETE answers 204 on success and 404 on a missing identity (`handler.go:173-185`). Real Azure answers 200 when the identity existed and 204 when it did not, and never 404 `(verify)`.
- PUT runs its resource group check through the legacy `rg:` key (`handler.go:73`) and returns 404 `ResourceGroupNotFound`. The engine check supersedes it.
- `principalId` and `clientId` are derived deterministically from the resource id (`deterministicUUID`, `handler.go:99-100`), and `tenantId` is a constant (`00000000-0000-0000-0000-000000000001`). Derivation keeps them stable across PUT, but delete and recreate yields the same ids; real Azure issues new ones.
- PATCH updates tags only (`handler.go:126`), and `isolationScope` is not modelled. PUT replaces tags wholesale.
- Existing coverage includes `internal/services/identity/handler_test.go` (`TestUpdateIdentityDoesNotMutatePublishedRecord`, `TestUserAssignedIdentityLifecycleAndPatchTags`) and the azurerm fixture `tests/terraform/azurerm/identity-rbac/main.tf` (an identity plus role assignments). There is no federated credential fixture.

## Scenarios

### userAssignedIdentities

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T109 | Create an identity. | `PUT userAssignedIdentities/{n}` returns 201; `properties.principalId`, `clientId` and `tenantId` are GUIDs; `location` and `type` match what was sent. | T2 | Already covered by the existing handler tests; port to an `armtest` `Sample` row. Assert GUID shape only, not the tenant value. |
| G2-T110 | Create in a missing resource group. | `PUT` returns 404 `ResourceGroupNotFound`. | T1 | Engine RG existence check. Already covered. |
| G2-T111 | Repeat the same PUT. | The second `PUT` returns 200 (not 201), and `principalId` and `clientId` stay unchanged. | T1 | |
| G2-T112 | Tags at create and on PATCH. | `PUT` with tags returns 201 and stores tags; `PATCH` with `{tags}` returns 200 and updates tags while leaving other properties unchanged. | T1 | Whether PATCH replaces or merges the tag set is `(verify)`. `PatchMerge` merges key by key. |
| G2-T113 | List by RG and by subscription. | `GET .../resourceGroups/{rg}/providers/Microsoft.ManagedIdentity/userAssignedIdentities` returns 200 with only that group's identities; `GET /subscriptions/{s}/providers/Microsoft.ManagedIdentity/userAssignedIdentities` returns 200 with all identities. | T1 | Subscription list is new and needs `ListSubscription` (PLAN 1.2). |
| G2-T114 | Two identities are distinct. | `principalId` and `clientId` differ between identities. | T2 | Derive from the resource id so distinct inputs produce distinct outputs. |
| G2-T120 | azurerm and azapi create and destroy an identity, and add a federated credential. | Apply and destroy succeed; tags are preserved; `azurerm_federated_identity_credential` (`new`) applies, reads back `issuer`, `subject`, and `audience`, and destroys. | T3 | Extend `tests/terraform/azurerm/identity-rbac/main.tf` and add an azapi variant. The federated credential part is `new` and needs the FIC scenarios first. |
| G2-N13 | `new`. Delete an identity. | `DELETE` existing returns 200 and `GET` after returns 404 `ResourceNotFound`; `DELETE` again returns 204. | T1 | `new`. Today it answers 204 then 404 (`handler.go:173-185`). |
| G2-N14 | `new`. Isolation scope. | `PUT` with `properties.isolationScope` set to `Regional` returns 201 and `GET` returns it; omitting it behaves as `None` `(verify)`. | T2 | `new`. Spec enum confirmed (`None`, `Regional`); default is `(verify)`. |

### federatedIdentityCredentials

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T116 | Create, get, list, update, and delete a credential. | `PUT .../federatedIdentityCredentials/{n}` returns 201; update returns 200; `GET` round-trips `properties.issuer`, `subject`, and `audiences`; list shows it; `DELETE` returns 200 and `GET` after returns 404. | T2 | Passthrough child. A body is required. Name pattern is enforced by `Validate`. |
| G2-T117 | Missing parent identity and missing credential. | `PUT` or `GET` of a credential under a missing identity returns 404 (`ParentResourceNotFound` per the engine rule; the real provider may return `ResourceNotFound` `(verify)`); `GET` of a missing credential returns 404 `ResourceNotFound`. | T1 | Service-specific not-found codes are not real ARM codes. |
| G2-T118 | PUT without `properties`, or missing a required field. | `PUT` returns 400 `InvalidRequestContent` `(verify)`; the spec requires `issuer`, `subject` and `audiences`. | T2 | Error code follows ADR-0006; the real provider's own code is `(verify)`. |

### identities/default

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T119 | Read the system-assigned identity of a resource. | `GET {scope}/providers/Microsoft.ManagedIdentity/identities/default` on a resource with no system identity returns 404 `IdentityNotFound` `(verify)`; with one it returns 200 with `location` present and `principalId` and `tenantId` matching the resource's `identity` block. | T2 bespoke | Bespoke. Depends on other services' `identity` blocks. |

## Pitfalls

- `principalId` and `clientId` are stable for the life of the identity, and different after delete and recreate. A deterministic derivation from the resource id gets the first and not the second.
- Real DELETE returns 200 when the resource existed and 204 when it did not. It never returns 404.
- The not-found code for a missing parent is `ParentResourceNotFound` in the engine and `ResourceNotFound` for the resource itself. A per-service code such as `ManagedIdentityNotFound` is not a real ARM code.
- PUT returns 201 on create and 200 on replace.
- Whether a missing identity named in another resource's `identity.userAssignedIdentities` is rejected or echoed by real Azure is `(verify)`. Decide once and test it where the consumer lives.
- A missing or malformed api-version returns 400; any well-formed version is accepted.
- A federated credential's `audiences` is an array (azurerm exposes it as `audience`); `issuer` must be a valid HTTPS URI `(verify)`.

## Dropped

| ID | Reason |
|---|---|
| G2-T115 | Asserts a service principal exists in the directory after creation. Needs an Entra or Graph service and is out of scope. |
