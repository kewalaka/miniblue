# ADR-0002: unregistered types are strict; passthrough is designed but deferred

Status: Proposed (strict default). Passthrough deferred until a concrete consumer exists.

## Context

azapi is a generic client that supports arbitrary `type@apiVersion` values, body passthrough, read-back after PUT, POST actions, read-modify-write updates, and list. Verified against the azapi provider source (v2.8 and upstream main):

- **Client-side gating.** `schema_validation_enabled` (default true) validates type and api-version against embedded bicep-types before any request. A typo never reaches the server.
- **Create.** Sends GET (404 means proceed), then PUT (200, 201, or 202 accepted), then GET read-back. A 404 on read-back drops the resource from state.
- **Sync polling.** The azcore poller treats a 200 or 201 without async headers as complete.
- **Read.** Merges the response into the configured body (`ignore_missing_property` defaults to true), so extra server fields do not cause drift. A location change forces replacement.
- **Update (`azapi_update_resource`).** Sends GET, merges the body, sends a full PUT, and runs GET. It does not send PATCH.
- **Delete.** Accepts 200, 202, and 204, and tolerates 404.
- **Action (`azapi_resource_action`).** Sends any method to `{id}/{action}` and accepts 200, 201, 202, and 204.
- **List (`azapi_resource_list`).** Sends GET `{parentId}/providers/{type}` (including subscription-wide) and follows `nextLink`.

So azapi needs a GET that returns 404 for missing resources, round-trip storage, full-PUT replace, sync status codes, and lists at parent and subscription scope.

## Decision

**Strict by default.** An unregistered `Ns/type` returns 404 `InvalidResourceType` (verify the exact ARM code; `NoRegisteredProviderFound` is the candidate) through the normal chi NotFound path.

**Deferred passthrough (`MINIBLUE_ARM_PASSTHROUGH=1`).** Do not build this until a user brings an azapi config that needs a type nobody intends to register. Every planned type is registered, so until then passthrough would be untested code with no caller. The design is recorded here so the eventual PR is S-sized and nobody invents a different shape. The chi `NotFound` handler tries the passthrough engine last:

```go
// internal/arm/passthrough.go
func (e *Engine) Passthrough() http.Handler // synthesises a Type per request
```

- **Synthesised `Type`.** Allows any scope, uses `Patch: PatchMerge`, and has no hooks. `api-version` is required and not checked against a list.
- **Operations.** Supports PUT, GET, DELETE, parent list, and subscription-wide list. POST actions return 404, because fabricated action results mislead users more than a clear failure.
- **Storage.** Uses the same `arm:r:` keys as registered types. When the type is registered later, existing data is served unchanged.
- **Logging.** Logs one `WARN` per new type: `passthrough type Microsoft.Foo/bars; register it for fidelity`.
- **Precedence.** Reached via NotFound, so it can never shadow a legacy chi route or a registered type.

## Consequences

- Clear failures by default, with no silent fake fidelity.
- When passthrough is built, azapi users can unblock themselves for long-tail types with one env var, and its WARN logs show what to register next.
- Implementation is about 60 lines with no new dependency.

## Rejected alternatives

- **Permissive by default.** Typos in tests that bypass azapi validation (raw SDK, REST) would succeed silently. That hides gaps and invites "works in miniblue, fails in Azure".
- **Validate bodies against bicep-types.** This adds a large embedded dataset (tens of MB) or startup cost. azapi already validates bodies client-side.
- **Passthrough via engine middleware (before chi).** This would shadow legacy handlers.
- **Fabricated 200 for POST actions.** Wrong data that looks plausible is worse than a 404.
- **Build passthrough in Phase 1.** No user or planned type needs it yet.
