# ADR-0004: Storage contract

Status: Proposed

## Context

`internal/store` has three backends (memory, file, Postgres) behind an `interface{}` API used at 424 call sites in 47 files. They disagree on what a value **is**:

- **Memory stores Go values by reference.**
- **File wraps memory** (`file.go:13`). It holds live structs until restart, then `load` (`file.go:39`) decodes into `interface{}`, giving maps and float64s, and ignores decode errors.
- **Postgres always returns decoded `interface{}`.**
- **Struct-asserting handlers break after a `PERSISTENCE=1` restart, and always on Postgres.**
    - They panic at `resourcegroups/handler.go:105` and `blob/handler.go:568,613,653,755,782`.
    - They silently drop records at `acr:224`, `keyvault:574,654`, `appconfig:119`, `dbpostgres:264` and `blob:212,353`.
    - The test suite runs memory only and never restarts, so it cannot see this.
- **Mutating a value after `Get` changes memory without `Set`.** `Save` marshals the live map while handlers mutate those values, which is a data race.
- **`ListKeysByPrefix` (`memory.go:177-182`) returns all keys** for file and Postgres.
- **`Set` (`memory.go:142`) swallows errors.** `SetIfNotExists` (`:149`) is a non-atomic Exists-then-Set.
- **Blob payloads are not persisted.** `Blob.Content` is tagged `json:"-"` (`blob/handler.go:30`). Memory holds the live struct, so tests pass, but every blob is empty after a `PERSISTENCE=1` restart and on Postgres. Any JSON-encoding store change makes this fail in memory too.
- **A corrupt snapshot starts empty, and autosave then overwrites it** (`file.go:33-36`). Per-value decode errors are ignored (`file.go:39`).
- **Postgres prefix matching is inconsistent.** `ListByPrefix` (`postgres.go:96`) and `DeleteByPrefix` (`:143`) use unescaped `LIKE`, while `CountByPrefix` (`:120`) escapes. There is no index usable for prefix `LIKE`.

The engine store (`internal/arm/store.go`) keeps docs at `arm:r:<lower id>` as JSON strings, plus a list index as one value per `arm:i:<parent>|<type>`. That index is a hot row, has no subscription-wide view, and `DeleteTree` only runs for types with children.

RG delete (`resourcegroups/handler.go:141-195`):

- is a hard-coded prefix list;
- imports `aks` and `vm` for cleanup;
- misses keyvault and `arm:` keys.

`/resources` (`:215`) returns `[]`.

## Decision

### 1. Backends are byte KV stores; `Store` owns encoding

```go
package store

type KV struct {
    Key   string
    Value []byte // JSON
}

type Backend interface {
    Get(key string) ([]byte, bool, error)
    Set(key string, v []byte) error
    SetNX(key string, v []byte) (bool, error) // atomic; Postgres: ON CONFLICT DO NOTHING
    Delete(key string) (bool, error)
    Scan(prefix string) ([]KV, error) // sorted by key
    DeleteByPrefix(prefix string) (int, error)
    Reset() error
}
```

- The current 9-method Backend shrinks to 7. `Exists`, `List`, `ListByPrefix` and `CountByPrefix` become `Store` methods derived from `Get` and `Scan`.
- File keeps wrapping memory. Its snapshot becomes a plain copy of `map[string][]byte`, with no live structs and no race.
- **The snapshot file format is unchanged.** The loader already decodes `map[string]json.RawMessage` (`file.go:32`), and each `RawMessage` is now the stored value as is. The schema stamp (§5) is an ordinary key, so it works the same on file and Postgres, with no file envelope.
- **A snapshot that fails to load fails startup.** The file is left untouched. Starting empty and autosaving over the user's state is data loss.
- `Store.Set(key string, v any) error` marshals and returns an error. Existing statement calls still compile, so there is no churn at the call sites, and it logs on failure. The engine and every migrated handler **must** check it and return 500.
- `Store.Get(key string) (any, bool)` unmarshals fresh on every call, the same as file-after-restart and Postgres today (`json.Unmarshal` into `any`). Every backend now behaves identically, so **memory-only tests catch persistence bugs**. Its signature stays for the 424 legacy call sites; a backend error is logged and reported as a miss.
- **Engine and migrated code read through `Store.Lookup(key) ([]byte, bool, error)` and `Store.Scan`**, which propagate errors. A backend failure is a 500, never "not found" or "empty list". That matters most for RBAC and policy, where a swallowed scan error would read as "no deny".
- **No persisted type may use `json:"-"`.** `Blob.Content` becomes `json:"content"` (base64 in JSON, about 33% larger, which is fine at emulator scale). A blob restart test covers upload, copy and block commit. This lands **before** the byte flip.
- `ListByPrefix`, `ListKeysByPrefix` and `CountByPrefix` derive from `Scan`. That fixes finding #3 by construction.
- Typed access for handlers:

```go
func Decode[T any](v any) (T, error)                  // fast path: v.(T); else JSON round trip
func GetAs[T any](s *Store, key string) (T, bool, error)
```

Order matters:

1. Fix `Blob.Content` and add the restart round-trip test. It passes on current main for everything except blob.
2. Add `Decode` and fix the known struct-asserting sites. That works before and after the flip.
3. Flip memory to bytes.
4. Fix whatever the test suite then surfaces.

### 2. Postgres

- `key TEXT PRIMARY KEY, value JSONB` is unchanged. Bytes go in and out as JSONB text, with no `interface{}` round trip.
- One `likePrefix(s string) string` escape helper, used by `Scan`, `DeleteByPrefix` and `Count`.
- `CREATE INDEX IF NOT EXISTS kv_key_prefix ON kv (key text_pattern_ops)`. The PK btree cannot serve `LIKE 'x%'` under non-C collations.
- `Scan` uses `ORDER BY key`.
- **Single writer.** One miniblue process per database. A shared database across processes is unsupported and documented. The engine mutex is in-process.

### 3. Engine store

- **Drop `arm:i:` index keys.** Leftover keys in existing state are never read, so no schema bump is needed.
- Docs stay at `arm:r:<lower id>`, stored as a JSON **string**. This preserves exact numbers through Postgres JSONB and float64 decoding.

Lists come from a prefix `Scan` plus a depth filter:

| List | Prefix | Filter |
|---|---|---|
| Top-level at scope | `<scope>/providers/<ns>/<type>/` | no further `/` |
| Child | `<parent>/<childType>/` | no further `/` |
| Subscription-wide, RG-scoped type | `/subscriptions/<s>/resourcegroups/` | parsed type == T, depth 1 |
| `/resources` in RG | `/subscriptions/<s>/resourcegroups/<rg>/providers/` | top-level only |

- `DeleteTree(id)` **always** runs: `Delete(id)` plus `DeleteByPrefix(id + "/")`. The `/` boundary keeps `rg` from deleting `rg2`, and armtest checks it.
- `Engine.DeleteScope(scope ID)` deletes every engine doc under a scope (`DeleteByPrefix(scope + "/")`).
- `/resources` returns engine resources only. Legacy types appear as they migrate. That is better than `[]` today.

### 4. Cascade via scope cleaners

```go
// package resourcegroups
type Cleaner func(sub, rg string)

func New(st *store.Store, eng *arm.Engine, cleaners ...Cleaner) *Handler
```

- Each legacy service exports its own cleaner, for example `aks.CleanupRG` or `keyvault.CleanupRG` (the latter is added, since it is missing today).
- The server builds the list.
- `resourcegroups` stops importing `aks` and `vm`.
- RG delete runs `eng.DeleteScope(rg)` and then the cleaners.
- A migrated type deletes its cleaner in the same PR.

### 5. State is disposable: a schema stamp, not migrations

Persisted state is test data. It does not need to survive an upgrade that changes its shape, but it must never be silently misread. Misread state is how a migrated type returns 404 and Terraform quietly recreates resources.

```go
// internal/store/schema.go
const SchemaVersion = 1 // bump when the key or value shape of an existing persisted type changes

func CheckSchema(s *Store, location string) error // called once at startup, before serving
```

- **Empty store:** write `meta:schema` = `SchemaVersion`.
- **Stamp matches:** continue.
- **No stamp on a non-empty store:** treat it as version 1. That is state written by current main, which has the same shape. Stamp it and continue.
- **Stamp differs:** fail startup, leaving the store untouched. The message names both versions and how to reset, for example: `state in ./data/state.json is schema 2; this miniblue needs 3. Delete the file (or TRUNCATE kv on Postgres) and restart.` There is no auto-wipe flag.

Bump rules:

- Moving an existing type onto the engine bumps the version. Its keys change.
- Adding a new type, or a new field that old readers tolerate, does not.
- Every bump adds a CHANGELOG line: `state reset required: <why>`.

Test: one table test covering the empty, matching, unstamped and mismatched cases, asserting that the store is unchanged on mismatch.

## Consequences

- PERSISTENCE and Postgres become as reliable as memory, and the default test run proves it.
- Shared-map races disappear. The cost is a JSON encode or decode per call. That is negligible at emulator scale, so it is not benchmarked unless a regression shows up.
- Engine lists get simpler and gain subscription-wide and `/resources` for free.
- RG cascade becomes complete and stays decoupled.
- Upgrades that move a type need a state reset. Startup says so explicitly instead of serving 404s.

## Rejected alternatives

- **Rewrite all 424 call sites to typed repositories now:** right in the long term, wrong before expansion. The uniform contract plus `Decode[T]` gets the safety at a fraction of the cost.
- **Keep the list index and add subscription and type indexes:** more keys to keep consistent, plus hot rows, to speed up scans that are already cheap.
- **Postgres-native schema** (`sub`, `rg` and `type` columns): backend-specific behaviour and migrations, for no emulator benefit.
- **Store `arm:` docs as JSONB objects:** float64 decoding corrupts large integers and json.Number semantics.
- **RG cascade via global `DeleteByPrefix` on every service prefix:** this is today's approach, and it is exactly what misses keyvault.
- **Lazy, on-read migration of legacy keys:** each type would need read fallbacks forever, which defeats the strangler exit.
- **Per-type data migrations** (a migrations registry, planned key moves, case-collision detection, fixture tests for each): real code and tests on every migrated type, all to preserve disposable test data. Decision: state does not need to survive shape changes.
- **Raw-byte side keys for blob content:** Postgres stores JSONB, so a raw value needs a second column or table. Base64 in JSON keeps one contract on every backend.
