# Deployments and ResourceManager

> Engine fit: bespoke for deployments and their actions (validate, cancel, `exportTemplate`, operations); hooks-style engine dispatch for the resources inside a template. miniblue today: hand-written service `internal/services/deployments` (resource group scope only, synchronous). Suggested wave: PLAN 3.1 (`refactor(deployments): dispatch in-process via Engine.Do`) and PLAN 3.2 (`feat(deployments): order resources by dependsOn`); extra scopes and actions after G3.

Deployments stay bespoke because they are an orchestrator, not a stored resource with a schema. They stop calling the router through a recorder and call `Engine.Do` instead, with an HTTP fallback for legacy types that forwards the original `Authorization` header (PLAN 3.1). The page also holds the resource group `exportTemplate` action, which is the other ResourceManager operation, and the shared resource id helper test.

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.Resources/deployments` | spec repo: 2024-11-01, 2025-03-01, 2025-04-01, 2026-06-01 (latest `(verify)`) | RG, subscription, management group, tenant | operations | `POST validate` (200/202/400), `POST cancel` (204), `POST exportTemplate` (200), `POST whatIf` (200/202), `HEAD` (204/404); DELETE (202/204) | bespoke | `Engine.Do` in-process dispatch (PLAN 3.1), topo sort (PLAN 3.2), `ChildID`, template expression evaluator | H | L |
| `deployments/operations` | same | same four scopes | none | list, GET by `operationId` | bespoke | per-resource operation records written during dispatch | M | S |
| `Microsoft.Resources/resourceGroups` `exportTemplate` | resources spec 2025-04-01 `(verify)` | RG | none | `POST /subscriptions/{s}/resourcegroups/{rg}/exportTemplate` (200/202) | bespoke | enumerate every resource in the RG (needs the `/resources` list from PLAN 3.3) | M | M |

Real facts confirmed in the REST spec (api-version 2025-04-01):

- Deployment name is 1 to 64 characters matching `^[-\w\._\(\)]+$`.
- `mode` is `Incremental` or `Complete`. Complete deletes resources in the group that are not in the template.
- `provisioningState` enum: `NotSpecified`, `Accepted`, `Running`, `Ready`, `Creating`, `Created`, `Deleting`, `Deleted`, `Canceled`, `Failed`, `Succeeded`, `Updating`.
- Cancel is allowed only while the state is `Accepted` or `Running`; afterwards the state is `Canceled` and resources are left partially deployed.
- Deleting a deployment removes its operations. A running deployment cannot be deleted.
- Deployment what-if change types: `Create`, `Delete`, `Ignore`, `Deploy`, `NoChange`, `Modify`, `Unsupported`.
- Management group and tenant scopes use `/providers/Microsoft.Management/managementGroups/{groupId}/providers/Microsoft.Resources/deployments/{n}` and `/providers/Microsoft.Resources/deployments/{n}`. Operations list and GET exist at all four scopes in the spec.

## miniblue gaps

Ordered by value.

- **Resources dispatch through an in-memory router, not the engine.** `handler.go:211-223` builds an `httptest` request per resource. Nested ids are built ad hoc and `dependsOn` is ignored (index finding 11). PLAN 3.1 replaces this with `Engine.Do`.
- **Missing `apiVersion` silently defaults to `2023-01-01`** (`handler.go:188`). Real Azure rejects the template. PLAN 3.1 returns 400 `InvalidTemplate` `(verify)`.
- **No ordering.** Resources deploy in template order; `dependsOn`, `copy` and `condition` are ignored. PLAN 3.2 adds a topo sort; a cycle gives 400.
- **Outputs are always `{}`** (`handler.go:124`). Expression support is limited to a whole-string `[parameters('x')]` or `[variables('x')]` (`template.go`); no function calls, concatenation, `reference()`, `resourceGroup()` or `tenant()`.
- **Resource group scope only.** Routes: list, PUT, GET, DELETE (with a duplicate case-variant set). No subscription, management group or tenant scope; no `HEAD`; no validate, cancel, whatIf, `exportTemplate` or operations.
- **Failure shape is thin.** A dispatch failure sets the deployment to `Failed` with error code `DeploymentFailed`; nested errors are not surfaced in `error.details`.
- **No template validation.** A missing `template` is a 400, but there is no check for the `$schema`, `contentVersion` and `resources` members.
- **DELETE returns 204** and does not remove operations (there are none). Deployments are not removed when their resource group is deleted (see Resource groups).
- Existing coverage: `internal/services/deployments/handler_test.go` (`TestSimpleDeploymentDispatchesEachResource`, `TestParameterAndVariableSubstitution`, `TestDispatcherFailureMarksDeploymentFailed`, `TestGetDelete`).

## Scenarios

### Validate, cancel and DELETE

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T23 | Validate a valid template succeeds. Validate `{"hello":"world"}` (no `$schema`, `contentVersion`, `resources`) fails. | POST `.../deployments/{n}/validate` => 200, `properties.provisioningState=Succeeded`; invalid => 400 `InvalidTemplate` `(verify)` | T2 | The spec lists 400 for validate. Sync only: no 202. Validate runs the same checks as PUT without dispatching. |
| G3-T24 | Cancel a deployment that has already finished fails. Cancel an unknown deployment fails. | POST `.../cancel` => 409 `DeploymentCannotBeCancelled` `(verify)`; unknown => 404 `DeploymentNotFound` `(verify)` | T2 | The spec only documents 204 for cancel; both error codes are unconfirmed. With sync dispatch every deployment is already terminal, so 409 is the normal outcome. |
| G3-N1 | `new`: PUT, GET, HEAD, list and DELETE at resource group scope. DELETE then GET and HEAD report not found. | PUT => 201 then 200; GET => 200; HEAD => 204 then 404; DELETE => 200 or 204 (spec 202/204; sync); GET missing => 404 `DeploymentNotFound` `(verify)` | T2 | Missing from the source. `HEAD` is absent today. Accept 200 or 204 on DELETE until a decision is recorded; do not return 202. |

### Export template

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T26 | Export the template of a finished deployment returns the original template. Unknown deployment fails. | POST `.../deployments/{n}/exportTemplate` => 200, body `template` equals the submitted template; unknown => 404 `DeploymentNotFound` `(verify)` | T2 | Needs the deployment record to keep the submitted template. |
| G3-T27 | Resource group export. An empty RG exports `resources=[]` plus `$schema`. With several resources, `resources:["*"]` exports all and a specific resource id exports just that one. | POST `/subscriptions/{s}/resourcegroups/{rg}/exportTemplate` => 200, `template.resources` per request | T2 | Bespoke. Depends on listing every resource in the RG (PLAN 3.3). Real Azure may add an `error` member for resources it could not export `(verify)`. Spec also allows 202; stay 200. |
| G3-T28 | Export options change the output: `IncludeParameterDefaultValue`, `IncludeComments` (adds a description in metadata), `SkipResourceNameParameterization` (literal names), `SkipAllParameterization` (no parameters). Managed identity name and location are parameterized by default. | POST `exportTemplate` with `options` => 200 with the matching shape | T2 | Option names and the identity parameterization are `(verify)` against the export request model. Low priority. |

### Scopes

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T29 | Subscription scope: create and list deployments at subscription scope. A template containing a `Microsoft.Resources/resourceGroups` resource creates that RG. | PUT `/subscriptions/{s}/providers/Microsoft.Resources/deployments/{n}` => 200 or 201; GET list => 200; the RG then exists | T2 | The spec allows 200 and 201. Subscription, management group and tenant deployments take a `location` in the body `(verify)`. |
| G3-T31 | Tenant scope: create returns the tenant-scoped id; GET; DELETE (no content); HEAD existence 204; validate `Succeeded`; list with no deployments returns an empty array. | PUT `/providers/Microsoft.Resources/deployments/{n}` => 200 or 201; GET => 200; DELETE => 200 or 204; HEAD => 204; list => 200, `value=[]` | T2 | Low value for an emulator. A tenant id has an empty namespace (`ID.IsTenant()`), so the id parses already. |

### Operations

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T32 | Operations of a deployment: one entry per deployed resource; an empty template gives none; GET by operation id returns it; unknown deployment and unknown operation fail. Repeat at all four scopes. | GET `.../deployments/{n}/operations` => 200; GET `.../operations/{id}` => 200; unknown deployment or operation => 404 `(verify)` code | T2 | Bespoke records written during dispatch. Operation body fields (`provisioningOperation`, `provisioningState`, `targetResource`) are `(verify)`. Management group scope needs a management group concept (see Dropped). |

### Template evaluation and outputs

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T33 | Outputs: a literal output, an output taken from a parameter, and `tenant()` used as a parameter default returns the default tenant id. | GET deployment => 200, `properties.outputs.<n>={type,value}` | T2 | Needs an expression evaluator beyond today's whole-string substitution (`handler.go:124` returns `{}`). Default tenant is `...0001` in miniblue. |
| G3-T34 | `reference()` of a Key Vault in the same template resolves `properties.vaultUri` in an output. | GET deployment => 200, output equals the vault's `vaultUri` | T2 | `reference()` is read through `Engine.Do` GET (or the HTTP fallback for a legacy type). A `reference()` to a resource that does not exist fails the deployment in real Azure, not a null output `(verify)`; see Pitfalls. |
| G3-T35 | Nested deployment (a `Microsoft.Resources/deployments` resource) at subscription scope provisions its inner resources. When an inner step fails, the nested deployment is `Failed` and the outer deployment reaches a terminal state (`Failed`) with the error carried in `error.details`. | GET outer and nested => 200; states `Succeeded` or `Failed` as above | T2 | Nested ids come from `ChildID`. Inner failure shape (`DeploymentFailed` wrapper) `(verify)`. |
| G3-T37 | A template containing `Microsoft.Sql/servers` deploys and the server can then be read. | PUT deployment => 200 or 201; GET `.../Microsoft.Sql/servers/{n}` => 200 | T2 | Cross-type dispatch test for the SQL page. Runs over the HTTP fallback until SQL migrates. |

### Dispatch and ordering (PLAN 3.1, 3.2)

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-N2 | `new`: `dependsOn` orders deployment: resource B that depends on A (by resource id or by name) is created after A, across an engine type and a legacy type. Template order does not matter. | PUT deployment => 200 or 201; creation order A then B; both GET => 200 | T2 | Acceptance for PLAN Gate G3 (`dependsOn` across an engine type and a legacy type). Observe order through the deployment's operations or resource creation timestamps `(verify)`. |
| G3-N3 | `new`: a `dependsOn` cycle is rejected before anything is deployed. | PUT deployment => 400 `InvalidTemplate` `(verify)` | T2 | PLAN 3.2. No resource may be created. |
| G3-N4 | `new`: a template resource without `apiVersion` is rejected. | PUT deployment => 400 `InvalidTemplate` `(verify)` | T2 | PLAN 3.1. Replaces today's silent `2023-01-01` default. |
| G3-N5 | `new`: template-only keys never reach the stored resource: `apiVersion`, `type`, `dependsOn`, `condition`, `copy`, `comments`, `scope` are stripped before dispatch. | GET deployed resource => 200, body has none of those keys | T2 | PLAN 3.1. |

### Shared resource id helper

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T55 | Build a resource id from a scope, a multi-level type and its names. `Microsoft.ServiceBus/namespaces/topics/subscriptions/rules` with name `ns/topic/sub/rule` gives the full id. A top-level type gives the short form. An empty (tenant) scope gives `/providers/...`. A type with a different number of segments than the name is not silently joined. | unit: chaining `arm.ChildID` from the scope through each level yields the full id, and `arm.ParseID` of the result round-trips to the same string; mismatched segment counts are rejected `(verify)` against the `ChildID` contract | T1 | Unit test in `internal/arm/*_test.go`, not an HTTP scenario. `arm.ParseID` exists; the builder lands in PLAN 1.4 (`ChildID`). Used by deployments for PUT and `outputResources`. |

## Pitfalls

- A deployment with a missing `apiVersion` on a resource, or a `dependsOn` cycle, fails validation before any resource is created. Defaulting the version hides template bugs that real Azure surfaces.
- Real Azure PUT on a deployment is long-running; a synchronous emulator returns the terminal body at once with 200 or 201. Do not return 202 or a `Running` state: clients poll forever if the poll URL is never completed.
- `reference()` of a resource that does not exist, or whose type the template engine cannot read, fails the deployment. Resolving it to null and succeeding hides real template errors.
- Cancel works only on `Accepted` or `Running` deployments and returns 204. A synchronous emulator has no running deployments, so cancel of a finished one is a conflict, not a no-op.
- Deleting a resource group removes its deployments and their operations. Leaving them behind makes a recreated group show stale history.
- A deployment name is limited to 64 characters of `[-\w._()]`; resource names inside the template follow their own type's rules.
- `dependsOn` entries are either resource ids or bare resource names; both forms resolve to the same node.
- Nested deployments take their own scope and `apiVersion`; inner ids must be built from the inner scope, not the outer one.
- What-if is a long-running diff operation; it needs the async machinery that ADR-0006 deliberately leaves out.

## Dropped

| ID | Reason |
|---|---|
| G3-T25 | Cancelling a running deployment needs asynchronous orchestration. Sync-only dispatch (ADR-0006) never leaves a deployment in `Running`, so there is nothing to cancel. Revisit if deployments become async. |
| G3-T30 | Management group scope. Management groups are not modelled and `Microsoft.Management` is out of scope; listing for an unknown group cannot be asserted without that store. |
| G3-T36 | What-if is a long-running operation (200/202) with a bespoke diff engine (`Create`, `NoChange`, `Modify`, `Delete`). Out of scope for the ARM foundation per SPIKE section 0; needs async (ADR-0006). |
