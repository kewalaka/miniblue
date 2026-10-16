# ARM Resource Engine

Most miniblue services are hand-written chi route trees over `map[string]interface{}` bodies. That does not scale to azapi-driven Terraform (arbitrary bodies, 100+ resource types, resources at management-group scope). `internal/arm` is a generic ARM resource engine that serves registered resource types with one shared implementation. It is introduced incrementally and coexists with the existing handlers.

Status: the engine serves `Microsoft.Authorization/policyDefinitions@2023-04-01` at subscription scope. Everything else still uses its own handler.

## Request flow

The engine is a chi **root middleware**, registered in `server.setupRoutes` before the authorization middleware:

```
request -> CORS/logging/recover/request-id/headers -> arm.Engine -> authorization middleware -> chi routes
                                                         |
                                       registered type at an allowed scope? -> serve
                                       anything else                         -> next
```

For every request the engine parses the path with `arm.ParseID`. It serves the request only if the type chain (`Namespace/type[/child...]`) is registered **and** the scope is one the type allows. Anything else, including unparsable paths, goes to `next` unchanged.

Why a middleware and not chi sub-routes:

- A registered type claims its own paths, so there is no route conflict with the existing `/subscriptions/{sub}/resourceGroups/...` trees. Migrating a handler is "register its type, delete its chi routes".
- It must run before the authorization middleware, which 404s every `Microsoft.Authorization` path except role assignments and definitions.
- It inherits request IDs, structured logging, panic recovery and Azure response headers.
- It does not depend on `casemirror.go`: the parser is case-insensitive itself, and casemirror only rewrites paths that match a chi route, which engine paths do not. The engine works with or without it.

Services are gated like the others: types are registered only when the `policy` service is enabled (`SERVICES` env var, default all).

## Resource IDs

`arm.ParseID` is case-insensitive and scope-agnostic. Keywords are canonicalised, names keep the casing they were parsed with. Subscriptions, resource groups and management groups are modelled as resources, so a scope is just another ID.

| Shape | Example |
|-------|---------|
| Tenant | `/providers/Microsoft.Authorization/policyDefinitions/x` |
| Management group | `/providers/Microsoft.Management/managementGroups/{mg}/providers/.../x` |
| Subscription | `/subscriptions/{sub}/providers/.../x` |
| Resource group | `/subscriptions/{sub}/resourceGroups/{rg}/providers/.../x` |
| Nested child | `.../virtualNetworks/{v}/subnets/{s}` |
| Extension resource | `{any resource ID}/providers/Microsoft.Authorization/.../x` |
| Collection (list) | `{scope}/providers/{ns}/{type}` |

The last `providers/{ns}/{type}` group is the resource; everything before it is its scope, parsed recursively. `ID.ScopeKind()` classifies the scope, `ID.Key()` is the lowercased identity.

## Store

`arm.Store` sits on `store.Store`, so it works with the memory, file and Postgres backends.

| Key | Value |
|-----|-------|
| `arm:r:<lowercased id>` | the resource document (as returned to clients) |
| `arm:i:<lowercased parent>\|<lowercased type>` | sorted list of child resource keys |

- Lookups are case-insensitive; the stored document keeps the casing of the PUT that created it, so `id` and `name` never change with the casing of later requests.
- Listing reads the index and then each child: O(children). It does not call `ListByPrefix`, which is O(n) on the memory backend (upstream issue #95).
- Only `DeleteTree` scans by prefix. The engine uses it only for types that declare `Children`, never on a plain delete.
- Documents are deep-copied on read and write (the memory backend stores by reference; Postgres round-trips JSON).
- State lives only in the backend, so `/_miniblue/reset` clears it.

## Behaviour

| Request | Result |
|---------|--------|
| `PUT {id}` | `201` on create, `200` on update. Full replace. |
| `GET {id}` | `200`, or `404 ResourceNotFound` |
| `DELETE {id}` | `200` if it existed, `204` if not |
| `GET {collection}` | `200 {"value":[...]}`, sorted by ID |
| `PATCH {id}` | Only for types with `PatchTags`. Replaces `tags`, other properties are ignored. |
| other methods | `405 MethodNotAllowed` |

Bodies are raw passthrough plus the server-owned `id`, `name` and `type`. A body `name` or `id` that contradicts the URL is a `400`. An `api-version` that is not in the type's list is a `400 InvalidApiVersionParameter`; a missing `api-version` is accepted, like the rest of miniblue. Errors are written through `internal/azerr`.

Not done by the engine yet: parent existence checks (a subscription, resource group or management group is not required to exist), `provisioningState`, long-running operations, `$filter`/paging.

## Registering a type

```go
reg.Register(arm.Type{
    Name:        "Microsoft.Authorization/policyDefinitions",
    APIVersions: []string{"2023-04-01"},
    Scopes:      arm.ScopeSubscription, // | arm.ScopeManagementGroup | ...
})
```

| Field | Meaning |
|-------|---------|
| `Name` | `Namespace/type`, matched case-insensitively |
| `APIVersions` | accepted `api-version` values |
| `Scopes` | bitmask of allowed scopes: tenant, management group, subscription, resource group, resource (extension) |
| `Children` | nested types (`{Name: "subnets"}`); they inherit `APIVersions` and `Scopes` |
| `PatchTags` | enable `PATCH` of tags |
| `Hooks.Validate` | `func(ID, body) error`, a returned error becomes a `400` |
| `Hooks.Defaults` | `func(ID, body)`, fill server-side defaults after validation |

Hooks are optional and exist for clients that drift on fields the server does not fill (azurerm). azapi tolerates raw passthrough, so `policyDefinitions` has none. Registration panics on a malformed or duplicate type, so mistakes surface at startup. Put registrations in `internal/services/<name>/` and enable them in `server.setupRoutes` behind `serviceEnabled`.

Moving `policyDefinitions` to management-group scope is adding `arm.ScopeManagementGroup` to `Scopes`; the engine is covered at every scope by unit tests.

## Tests

- `internal/arm`: parser (every scope shape, casing, invalid paths), store (memory and a JSON round-trip backend), engine CRUD with synthetic types at every scope, children, hooks, PATCH, error cases.
- `tests/arm_policydefinition_test.go`: policyDefinitions through the real server, including coexistence with the authorization handlers.
- `tests/terraform/azapi/policy_definition`: the acceptance test. `terraform apply`, `terraform plan -detailed-exitcode` must report no drift, GET with mangled URL casing must return the canonical `id`, `terraform destroy`, GET must be `404`. It runs in CI (`.github/workflows/acceptance.yml`) with pinned Terraform and azapi versions.

To run the acceptance test locally (Linux/macOS/WSL):

```bash
LOCAL_AZURE_CERT_DIR=/tmp/miniblue-certs go run ./cmd/miniblue &
SSL_CERT_FILE=/tmp/miniblue-certs/cert.pem ./tests/terraform/azapi/policy_definition/run.sh
```

## Migrating an existing handler

1. Register the handler's types (api-versions, scopes, children) in a `Register(reg)` function.
2. Port the per-type behaviour that clients depend on into `Defaults`/`Validate` hooks. Keep the existing handler tests; they are the spec.
3. Delete the chi routes for those types. Roll them out one handler per PR.
4. Move any cascade (resource group delete, parent delete) onto `Store.DeleteTree` or the engine.

Good first candidates are `roleDefinitions` and `roleAssignments`; once they move, the authorization middleware can go.

## Follow-ups

- Management groups, multi-subscription/tenant model, RBAC at management-group scope.
- Policy assignments, initiatives and evaluation; built-in policy definitions (ALZ looks them up by GUID at tenant scope).
- Parent existence resolvers, extension-resource cascade, `provisioningState`/lifecycle hooks when a second type needs them.
- Migrating the existing handlers and then removing `casemirror.go`.
- Backend: `ListByPrefix` complexity (#95) and unescaped `LIKE` wildcards in the Postgres `ListByPrefix`.
