# ADR-0002: Unregistered types are strict; passthrough is designed but deferred

Status: Proposed (strict default). Passthrough deferred until a concrete consumer exists.

## Context

azapi is a generic client: any `type@apiVersion`, body passthrough, read-back after PUT, POST actions, read-modify-write updates, and list. Verified against the azapi provider source (v2.8 and upstream main):

- **Client-side gating.** `schema_validation_enabled` (default true) validates type and api-version against embedded bicep-types before any request. A typo never reaches the server.
- **Create:** GET (404 means proceed), then PUT (200, 201 or 202 accepted), then GET read-back. A 404 on read-back drops the resource from state.
- **Sync polling.** The azcore poller treats a 200/201 without async headers as complete.
- **Read:** the response is merged into the configured body (`ignore_missing_property` defaults to true), so extra server fields don't cause drift. A location change forces replacement.
- **Update** (`azapi_update_resource`): GET, then merge, then a full PUT, then GET. **No PATCH.**
- **Delete:** 200, 202 and 204 are accepted. 404 is tolerated.
- **Action** (`azapi_resource_action`): any method at `{id}/{action}`. 200, 201, 202 and 204 are accepted.
- **List** (`azapi_resource_list`): GET `{parentId}/providers/{type}` (including subscription-wide), following `nextLink`.

So azapi needs: a GET that returns 404 for missing resources, round-trip storage, full-PUT replace, sync status codes, and lists at parent and subscription scope.

## Decision

**Default: strict.** An unregistered `Ns/type` returns 404 `InvalidResourceType` (verify the exact ARM code; `NoRegisteredProviderFound` is the candidate) through the normal chi NotFound path.

**Deferred: `MINIBLUE_ARM_PASSTHROUGH=1`.** Not built until a user brings an azapi config that needs a type nobody intends to register. Every planned type is registered, so until then passthrough would be untested code with no caller. The design is recorded here so the eventual PR is S-sized and nobody invents a different shape. The chi `NotFound` handler tries the passthrough engine last:

```go
// internal/arm/passthrough.go
func (e *Engine) Passthrough() http.Handler // synthesises a Type per request
```

- Synthesised `Type`: any scope, `Patch: PatchMerge`, no hooks. `api-version` is required but not checked against a list.
- Ops: PUT, GET, DELETE, list at parent, list subscription-wide. **POST actions return 404.** Fabricated action results would mislead users more than a clear failure.
- Storage: the same `arm:r:` keys as registered types. When the type is registered later, existing data is served unchanged.
- Logs one `WARN` per new type: `passthrough type Microsoft.Foo/bars; register it for fidelity`.
- Because it is reached via NotFound, it can never shadow a legacy chi route or a registered type.

## Consequences

- Clear failures by default. No silent fake fidelity.
- When passthrough is built, azapi users can unblock themselves for long-tail types with one env var, and its WARN logs show what to register next.
- About 60 lines when built. No new dependency.

## Rejected alternatives

- **Permissive by default:** typos in tests that bypass azapi validation (raw SDK, REST) succeed silently. It hides gaps and invites "works in miniblue, fails in Azure".
- **Validate bodies against bicep-types:** a large embedded dataset (tens of MB) or startup cost, against the constraints. azapi already does this client-side.
- **Passthrough via engine middleware (before chi):** it would shadow legacy handlers.
- **Fabricated 200 for POST actions:** wrong data that looks plausible is worse than a 404.
- **Build passthrough now, in Phase 1:** speculative. No user or planned type needs it yet.
