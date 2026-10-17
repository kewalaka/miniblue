# ADR-0006: Synchronous LRO and error taxonomy

Status: Proposed

## Context

**LRO.** Real ARM returns 201 or 202 plus `Azure-AsyncOperation` or `Location` for long-running writes. Clients handle synchronous terminal responses fine:

- **azapi and azcore:** a 200 or 201 with no async headers is done.
- **go-azure-sdk (azurerm):** a 200 or 201 with `provisioningState: Succeeded` is done. Its provisioningState poller otherwise waits about 10 s per write, unless the response carries `X-Go-Azure-Sdk-Skip-Polling-Delay: true`. A bare 202 with no `Location` is a trap. `publicip` does exactly that.

**Errors.** `internal/azerr` (`errors.go`) writes `{"error":{"code","message"}}` through per-status helpers.

- There is no `error` type, so the engine and hooks cannot return typed failures.
- There is no `target` or `details`.
- `NotFound` says "under resource group" for every scope (`errors.go:25`).

## Decision

### Sync only

| Op | Status |
|---|---|
| PUT | 201 create, 200 replace |
| PATCH | 200 |
| DELETE | 200 when it existed, 204 when missing |
| Action | 200 with a body, 204 without |
| GET, List | 200 |

- The engine never returns 202 and never sends `Azure-AsyncOperation` or `Location`.
- `Provisioned` types stamp `provisioningState: Succeeded` (ADR-0003).
- `AzureHeaders` middleware sets `X-Go-Azure-Sdk-Skip-Polling-Delay: true` on every response. Other clients ignore it. This is the cheapest speed-up available for azurerm suites. Land it in Phase 0.
- Legacy async flows (RG delete `operationresults`, KV soft-delete) stay bespoke until they migrate.
- A future `Type.Async` is gated on a concrete client that fails without 202. None is known.

### One error type

```go
package azerr

type Error struct {
    Status  int     `json:"-"`
    Code    string  `json:"code"`
    Message string  `json:"message"`
    Target  string  `json:"target,omitempty"`
    Details []Error `json:"details,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func New(status int, code, format string, args ...any) *Error
func Write(w http.ResponseWriter, err error) // *Error as-is; anything else becomes 500 InternalServerError and is logged
```

- The existing helpers (`WriteError`, `NotFound`, `BadRequest` and so on) become one-line wrappers over `Write`. Legacy call sites are untouched.
- The engine writes errors only via `Write`. Hooks return `*azerr.Error` (ADR-0003 phase 8).
- `NotFound` messages are built from the `arm.ID`, scope-aware: "under resource group 'rg'" only at RG scope (verify exact ARM wording).

### Taxonomy (engine-emitted)

| Status | Codes |
|---|---|
| 400 | `InvalidRequestContent`, `MissingApiVersionParameter`, `InvalidApiVersionParameter`, `InvalidResourceName`, `InvalidParameter` (with `target`), plus type-specific codes from hooks |
| 403 | `AuthorizationFailed`, `RequestDisallowedByPolicy` |
| 404 | `ResourceNotFound`, `ResourceGroupNotFound`, `SubscriptionNotFound`, `ParentResourceNotFound`, `InvalidResourceType` (verify) |
| 405 | `MethodNotAllowed` |
| 409 | `Conflict`, plus type-specific codes (e.g. `RoleAssignmentExists`) |
| 412 | `PreconditionFailed` |
| 500 | `InternalServerError` |

Types add codes in their hooks. There is no central registry of codes.

## Consequences

- Deterministic, fast tests. azurerm and azapi both complete without polling.
- No fake operation-status endpoints to maintain.
- Hook authors get one obvious way to fail.

## Rejected alternatives

- **Simulated async (202 plus operation status):** more code and slower tests, with no client that needs it. A non-goal.
- **Configurable latency:** a non-goal. Real LRO timing is out of scope.
- **A separate `arm.Error`:** duplicates `azerr` and needs mapping both ways.
- **Sentinel errors plus an engine-side mapping table:** hooks cannot attach `target` or type-specific codes.
- **Sending the skip-polling header only on ARM writes:** needs path classification for zero benefit.
