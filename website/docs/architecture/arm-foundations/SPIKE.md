# Spike: what can we bring across using the ARM engine?

> **Status: input, partly superseded.** This spike came before the ARM foundation ADRs. Where they differ, the [ADRs](index.md) and the [PLAN](PLAN.md) win. §0 lists every change. The spike is still the source for the resource ranking (§3), the test tiers (§4) and the APIM provider findings (§5).

Inputs: a second Azure emulator used as inspiration only (its behaviour is not spec; about 480 scenarios catalogued), the landed `internal/arm` engine, and the azurerm provider source (for APIM). The detailed reports and scenario catalogues are not in the repo. Scenario ids below refer to them.

## 0. What the ADRs changed

| Spike said | Now | Where |
|---|---|---|
| Five missing engine primitives are the bottleneck | About 6 seams. The top risk is the store value contract: struct type-asserts panic or drop data after a `PERSISTENCE=1` restart and on Postgres | README verdict, ADR-0004 |
| E2: accept-any api-version option; types list every client version | Missing or malformed gives 400. Any well-formed version is accepted. `APIVersions` is metadata only | ADR-0005, PR 1.4 |
| E3: `Defaults(id, body, cur)` | `Request` carries `Current`. Hooks run under the write lock and return `azerr.Error` | ADR-0003, PR 1.1 |
| E4: PATCH deep-merge, per-type opt-in | `PatchMode`, RFC 7386 merge by default | ADR-0003, PR 1.3 |
| E6: POST actions as chi routes beside the engine | Instance actions belong to the engine (`Type.Actions`, lands with its first consumer). Only provider-level operations (`checkNameAvailability`, `locations/*`, `operations`) stay as chi routes | ADR-0001 |
| E7: `RequireParent` opt-in, never default-on | Parent and RG existence are checked for every engine type. Migration PRs fix tests that skip creating the RG | ADR-0003 phase 5, ADR-0007 |
| E0c: `/resources` from the registry plus legacy prefixes | From the engine, when RGs move onto it | PR 3.3 |
| Stay bespoke: RG, deployments, subscriptions, auth, keyvault, network, NICs, storage, dbpostgres, aci | RGs and subscriptions move onto the engine (wave 3). Role assignments move (Slice 1). Deployments stay bespoke but dispatch in-process. The ARM shells of keyvault, network, NICs, storage, dbpostgres and aci are wave 4 and optional. Data planes never move | ADR-0007, ADR-0001 |
| APIM needs no engine work and runs in parallel with it | Slice 2 starts after G1 and uses `Current` for stable generated values | Slice 2, PLAN |
| APIM chi routes for `checkNameAvailability` and `deletedservices` | The MVP needs neither. `deletedservices` is a strict 404 with no route. `checkNameAvailability` is a later PR | Slice 2 |
| `ParseID` must handle `;rev=` and `%3B` | `;rev=` works on the decoded path. An encoded `/` inside a name is still open | README verify list |
| Opt-in async LRO per type; RG delete stays 202 with `operationresults` | Sync only. RG delete becomes 200 when RGs move onto the engine | ADR-0006, PR 3.3 |
| Migrated types need a CHANGELOG note or a key shim | State is disposable. A schema version stamp fails startup with a reset message. No shims, no migrations | ADR-0004 §5 |
| Conformance suite carries a `CaseMode` | Engine responses are canonical. Moving a type off `casemirror` is a listed behaviour change | ADR-0005, ADR-0007 |
| Postgres `LIKE` wildcards unescaped (unverified) | Verified | README finding 8, PR 0.5 |
| §6 phasing | Replaced | PLAN |

## 1. Verdict

- **Yes, a lot ports.** ~25 resource types are plain registrations or hooks on the engine today or after small engine work; ~480 test scenarios were harvested (per-resource and cross-cutting scenario catalogues, not in the repo).
- **The engine, not the resources, is the bottleneck.** Five missing primitives gate ~25 rows: subscription-wide list, POST actions, parent-exists, typed hook errors, PATCH beyond tags. Land those first (S/M each).
- **APIM is feasible: medium effort, high value**, and the Consumption-SKU slice needs no APIM-specific engine seams (it starts after G1, see §0). ~4 to 6 dev-days; +2 for Developer/Basic/Standard/Premium.
- **The reference is a weak oracle**: no api-version validation, fully synchronous, per-service error codes, mandatory `If-Match` on APIM, several outright bugs. Port *scenarios*, assert real Azure status codes/bodies.
- **Existing hygiene bugs the spike found** (fix regardless): CI never runs `internal/...` tests; RG delete doesn't cascade into engine-stored resources; `GET …/resourceGroups/{rg}/resources` always returns `[]`; Redis PUT ignores the body; Key Vault PUT has no name/RG validation; LoadBalancer regenerates `resourceGuid` every PUT; `parity.md` is stale (App Service, App Config labels). All confirmed at review time; see README findings 6, 7, 15 and 21 to 24.

## 2. Engine work, in order

Superseded by PLAN Phase 1 (see §0). Kept for the Unblocks column.

| # | Item | Size | Unblocks |
|---|---|---|---|
| E0a | `go test ./...` in `ci.yml` (today `./tests/...` only) | XS | engine/store/id/deployments guarded |
| E0b | RG delete → `Store.DeleteTree(rgID)` for engine types; add cascade test | S | every RG-scoped engine type |
| E0c | Back `GET …/{rg}/resources` with engine registry + legacy prefixes | M | azurerm `prevent_deletion_if_contains_resources`, `az resource list` |
| E1 | Typed hook errors `{Status, Code, Message}`; optional `Type.NamePattern` | S | name rules, 409s, MG/KV/EH codes |
| E2 | api-version: accept-any option; types list every version azurerm/azapi/az send (verify pins per provider release) | S | newer azurerm/Bicep; deployments default `2023-01-01` otherwise 400s |
| E3 | Pass stored doc to hooks (`Defaults(id, body, cur)`) | S to M | stable generated values (keys, GUIDs, createdAt) without id-derivation hacks |
| E4 | PATCH deep-merge, per-type opt-in, re-run `Defaults` | M | in-place updates (azurerm `UpdateThenPoll`, azapi update, `az … update`) |
| E5 | Sub-scope list for RG-scoped types (`GET /subscriptions/{s}/providers/{ns}/{type}`) | M | every `ListBySubscription`, `az … list`, name-uniqueness |
| E6 | POST actions: bespoke chi routes beside the engine work today (unregistered action paths fall through); later `Type.Actions` | S per action / M generic | `listKeys`, `regenerateKey`, `listSecrets`, `checkNameAvailability` |
| E7 | Opt-in `RequireParent` (case-insensitive RG resolver; legacy tests PUT under unregistered RGs, so never default-on) | M | `ResourceGroupNotFound`/`ParentResourceNotFound` negative paths |
| E8 | Generic `provisioningState: Succeeded` default; sync 200/201 is enough for azurerm/azapi pollers. Async headers only where a poller truly needs a URL (disk `diskOperations`) | S to M | removes per-type boilerplate |
| E9 | `Registry.Types()` + `internal/arm/armtest` table-driven conformance suite (§4) | M | every type forced through one contract |
| Later | Singleton/auto-created children, secret redaction (`SecretPaths`), opt-in ETag/412 (never *require* `If-Match`), `$top/$skip/nextLink`, soft-delete hooks, stateful provider registration (+optional 409 gate) | M each | per-type, as needed |

Not needed: LRO/202 machinery for the MVP types, `$filter` beyond role definitions.

## 3. Resource candidates (ranked)

Fit: P plain registration (no hooks), H hooks, G engine-gap (needs E-items), B bespoke. Value = azurerm/azapi/bicep/SDK users. Scenario ids refer to the catalogues.

| Rank | Resource | Fit | Value | Effort | Needs | Scenarios | Notes |
|---|---|---|---|---|---|---|---|
| 1 | **API Management** (`service`, `apis`, `operations`, `products`, `namedValues`, `backends`) | H | H | M (4 to 6 d) | G1 (Slice 2) | `APIM-T*` (52) | see §5 |
| 2 | roleDefinitions / roleAssignments → engine | H | H | M | G1 (Slice 1) | `G1-T48…56` | narrows the authorization middleware to roleDefinitions; duplicate → 409 `RoleAssignmentExists` |
| 3 | Management Groups (+ policyDefinitions at MG scope = one-line scope-mask change) | H/B | H | M to L | E1, E7, delete/GET hooks, subscription-association move, root group | `G2-T1…33` | engine id model already parses tenant/MG scope; hierarchy endpoints (`descendants`, `getEntities` is **POST** in real Azure) bespoke beside the engine |
| 4 | Insights `components` + LogAnalytics `workspaces` | H | H | S each | E3 (stable workspaceId/instrumentationKey) | `G2-T63…81` | needed together by common azurerm modules |
| 5 | Event Hubs (`namespaces`, `eventhubs`, `authorizationRules`+listKeys, `networkRuleSets/default`) | H | H | S to M | E6 | `G2-T54…60` | AMQP data plane out of scope |
| 6 | SQL servers/databases (replace stub) + **`firewallRules`** | H | H | M | E1, E5, redaction of admin password | `G3-T56…76` | biggest gap vs reference: stub drops tags, FQDN `localhost`, no PATCH/list/tests; firewall rules not in reference but required by azurerm |
| 7 | ServiceBus `subscriptions`, `rules` (`$Default`), `authorizationRules`+listKeys | H | H | S to M | auto-children, E6 | `G3-T83…94` | ARM subscription DELETE required for azurerm destroy (reference lacks it) |
| 8 | ManagedIdentity `federatedIdentityCredentials`; Redis `firewallRules`; **fix Redis PUT body** | H | M to H | S | none | `G2-T113…127` | workload-identity/GitHub OIDC value |
| 9 | App Configuration ARM: 4-key set, PATCH, soft-delete/`deletedConfigurationStores`/purge (409 when protected), replicas | G | M | M | E6, soft-delete hook | `G1-T1…23` | data-plane labels/revisions/lock (409 key-locked)/snapshots: bespoke, `G1-T24…30` |
| 10 | ACR: name/location validation, PATCH, `listCredentials`/`regenerateCredential`, `listUsages` | H/G | M | S to M | E1, E6 | `G1-T79…97` | name 5 to 50 alnum |
| 11 | Cosmos ARM: `throughputSettings`, `InvalidKeyKind` | G | M | M | E6 | `G1-T98…110` | data-plane SQL query subset (~20 scenarios) is bespoke, L |
| 12 | Storage ARM: PATCH, `regenerateKey`, `checkNameAvailability`, SAS list actions, ARM queues/tables | B | M | M | keep keys + `creationTime` across PUT | `G3-T39…50` | stays bespoke (shared-key coupling, PUT must return 200) |
| 13 | Availability sets (+VM membership), VNet PATCH/peerings/delegations, subscription locations, provider registration state | H | M | S to M | side-effects for VM link | `G3-T101…118`, `X34…36` | |
| 14 | Disk `diskOperations` poll + 409 on active SAS; ACI start/stop/restart/logs + first ACI tests; App Service list-by-sub | G | L to M | S to M | E5, E6 | `G1-T33…78` | fills zero-test ACI gap |
| 15 | Key Vault keys + certificates data plane (Go `crypto/*`) | B | H | L | none | `G2-T94…102` | biggest data-plane gap; not an engine job |

Migrate-only (no new user value, do opportunistically): publicip, nsg(+securityRules child), eventgrid ARM, dbmysql stub, dns, disks. **Stay bespoke** (as revised by ADR-0007, see §0): data planes (blob/queue/table, KV secrets, and so on), deployments, and vm/aks/sites/apps unless a fidelity or enforcement need arises (G5). The ARM shells of keyvault, network (inline+child subnet sync), NICs, storage, dbpostgres and aci are wave 4.

**Skip**: LoadBalancer content (miniblue is already a superset; add PATCH tags + sub list only), RESP2/AMQP servers, RBAC data-plane enforcement, portal/MCP/CLI-wrapper suites, subscription create/cancel/PATCH (not real ARM).

## 4. Test cases to bring across

Method: scenarios only, re-written as our own tests asserting real-Azure contracts (reference tests are SDK-driven with weak assertions).

| Tier | Where | What | Count (approx) |
|---|---|---|---|
| T1 conformance | `internal/arm/armtest` (table-driven, needs `Registry.Types()`) | One sample per registered type; asserts PUT 201/200, GET, list, DELETE 200/204, canonical casing, missing or malformed api-version 400, name/id mismatch 400, children cascade, PATCH; options for ETag/paging; coverage test fails build if a type has no sample. Reference has no generic contract tests, so this is net-new | 25 (`X1…X27`) |
| T2 per-resource | `tests/arm_<x>_test.go` via real server (pattern: `tests/arm_policydefinition_test.go`) | Scenario catalogues (not in the repo), ported as raw-HTTP httptest | ~330 |
| T3 Terraform acceptance | `tests/terraform/{azurerm,azapi}/<case>/` | New: `azurerm/{apim-consumption,sql,servicebus,app_configuration,container_registry,cosmosdb,mgmt-group}`, `azapi/{servicebus,storage_account_basic,virtual_network_basic,apim}`, `azurerm_availability_set` in compute, negative fixture (invalid name must fail with surfaced 400). Batch azapi into one job (each fixture = a full CI job today); `TF_PLUGIN_CACHE_DIR`; nightly latest-provider | ~12 fixtures |
| T4 client smoke | `tests/sdk/go`, Python, scripted `az` | Go SDK (`armresources`, policy defs: LRO polling, ID casing, 404 after delete), Python SDK, `az cloud register` + version matrix. Harness: CA-bundle trust per client (`SSL_CERT_FILE`, `REQUESTS_CA_BUNDLE`, certifi for az, `AZURE_CORE_INSTANCE_DISCOVERY=false`); document the matrix in `acceptance-tests.md` | 3 suites |
| T5 deployments | extend bespoke handler | validate, exportTemplate, cancel/HEAD, subscription scope. what-if is out of scope (needs async, ADR-0006) | ~15 (`X40…48`) |

CI cost rule: T1/T2 run in default CI (seconds, in-process). T3 fixtures are opt-in per case; anything >2 min goes nightly. **Never run long-provisioning resources against real Azure from this repo's CI or from agents.**

## 5. API Management

**MVP (Consumption, `sku_name = "Consumption_0"`):** `Microsoft.ApiManagement/service` (2024-05-01 + extras) with children `apis`→`operations`, `products`→`apis`, `namedValues`, `backends` (children 2022-08-01, backends also 2024-05-01; add 2021-08-01, 2023-05-01-preview, 2023-09-01-preview, 2024-06-01-preview so az/bicep users don't get `InvalidApiVersionParameter`).

Findings that shape the design (from the provider source, v5.10.0):

- **No LRO needed.** Sync 200/201 + `provisioningState: Succeeded`, no `Azure-AsyncOperation`/`Location`. DELETE: 200 existed / 204 absent (engine already); a bare 202 routes azurerm into its own 20 s poller. Optional response header `X-Go-Azure-Sdk-Skip-Polling-Delay: true`. A malformed `Location` with `asyncId` on API PUT would trigger the provider's custom poller: never emit one.
- **Provider dereferences without nil checks** → `Defaults` must always supply: service `customProperties` (`{}`), api `authenticationSettings{oAuth2AuthenticationSettings:[],openidAuthenticationSettings:[]}`, `subscriptionKeyParameterNames{header:"Ocp-Apim-Subscription-Key",query:"subscription-key"}`, `protocols:["https"]`, `type:"http"`, `isCurrent`. Service defaults: `virtualNetworkType=None`, `publicNetworkAccess=Enabled`, derived `gatewayUrl`/`managementApiUrl`/`scmUrl`/`portalUrl`/`developerPortalUrl`, TEST-NET `publicIPAddresses`, identity via `DefaultIdentity` with `Current` (ADR-0003).
- **API names carry `;rev=N`** (`apis/echo;rev=1`): `ParseID` handles `;rev=` on the decoded path; keep a test that echoes the original id. Alias `apis/echo` ⇄ current revision for CLI/Bicep.
- **`If-Match`**: azurerm sends `*` only on API create/update, nothing on DELETE. Never require it (reference does → breaks `terraform destroy`). Optional 412 on mismatch behind an ETag flag.
- **Create flow also**: `GET deletedservices/{name}` (404/403 tolerated), list+delete of apis and products (must return `{"value":[]}`; don't seed any), so types must be registered. `deletedservices` stays always-404 in MVP; `purge_soft_delete_on_destroy` documented unsupported.
- **Provider-level routes, after the MVP** (ADR-0001 allows them; `deletedservices` needs none, strict 404 is enough): `POST …/providers/Microsoft.ApiManagement/checkNameAvailability` (regex `^[a-zA-Z][a-zA-Z0-9-]*[a-zA-Z0-9]$`, 1 to 50; `AlreadyExists` by subscription scan), `GET …/deletedservices/{n}`.
- **Tier B (`Developer_1`+)**: `portalsettings/{signin,signup,delegation}` (GET must work without a prior PUT; PUT returns 200 only), `POST portalsettings/delegation/listSecrets`, `tenant/access` GET/PATCH + `POST …/listSecrets` (`id`, `enabled`, `primaryKey`, `secondaryKey`). All seeded-default GETs.
- **Out of scope**: gateway runtime (reference has none), subscriptions/users/groups/policies/diagnostics/certs (+1 week), secret masking, backup/restore.
- **Test plan**: T1/T2 `APIM-T*` (name rules, required fields, `;rev`, defaults round-trip, product `state` honoured, cascade delete, checkName). T3: `azurerm/apim-consumption` fixture with `timeouts{}` blocks (real provider timeouts are 3 h create/update/delete, so the emulator must answer instantly or CI hangs); `Developer_1` fixture nightly only. Real APIM provisioning (30 to 60+ min; Consumption much faster) is documented, never run.
- Reference deviations to avoid: inverted `checkNameAvailability` validity, forced product `state`, malformed async `Location`, plaintext secrets.

## 6. Phasing

Replaced by [PLAN](PLAN.md). The cheap-win bundle in §3 (Insights, LogAnalytics, EventHub, MI FIC, Redis firewall, SQL firewall, ServiceBus subscriptions/rules, availability sets) is the first batch of new types after G2.

## 7. Risks / open items

- Resolved by the ADRs: migrated-type state (schema stamp, ADR-0004 §5), Postgres `LIKE` escaping (PR 0.5), casing (canonical, ADR-0005), deployments' default api-version and template keys (PR 3.1), authorization middleware ordering (overlap guard, PR 1.6).
- Terraform acceptance is Linux/WSL only; cold `go build ./...` ~3 min on this box. Iterate with in-process tests.
- **Verify before asserting** (could not fetch REST specs in this spike): APIM name/enum casing and error codes, latest api-versions per type, azurerm/azapi api-version pins per release, Management Group error codes, real-Azure status codes marked "(verify)" in the reports.
- Unverified reference inferences: SQL admin password echo on GET; expectations for non-MG group-2 tests came from names/control-plane code, not assertion bodies.
- Licence: the reference is Apache-2.0 (compatible with MIT). Only scenarios were extracted; no code or test bodies copied. If anything is ever copied verbatim, keep its notice/NOTICE obligations.
