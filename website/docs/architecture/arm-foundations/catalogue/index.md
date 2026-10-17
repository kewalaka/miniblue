# Scenario catalogue

Behaviour scenarios harvested from a review of a second Azure emulator, rewritten as real-Azure assertions and mapped to the ARM engine. Each page lists a service's resource types, miniblue's gaps and the scenarios a migration PR should port. Another emulator's behaviour is a prompt, not a spec: where it differs from real Azure, the page asserts real Azure and notes the pitfall.

493 scenario ids: 478 from the review (kept as is, so earlier references still resolve) and 15 marked `new`. Nothing was run against real Azure. About 450 `(verify)` marks remain, to be resolved against the REST specs or a single real call before the PR that depends on them.

## How to use it

A PR that registers or migrates a type:

1. Open the type's page. Read **Types** (fit, ADR seams needed) and **miniblue gaps**.
2. Port its T1 rows as the `armtest` `Sample` and its T2 rows as `tests/arm_<x>_test.go`, as in `tests/arm_policydefinition_test.go`. T3 rows become Terraform fixtures only where the page says so.
3. List the ids ported in the PR description. Resolve each `(verify)` you touch, or leave it.
4. Do not port a row marked Dropped, `bespoke` data plane (unless the PR is about it) or "do not implement".

Ids: `G1-T*`, `G2-T*`, `G3-T*` by service group, `APIM-T*`, `X*` for cross-cutting. `*-N*` is a scenario added during cataloguing. Tiers: T1 engine conformance (`armtest`, generic), T2 per-resource HTTP test in-process, T3 Terraform acceptance (opt-in, nightly if over about 2 minutes), T4 client smoke (SDK or `az`), T5 deployments.

**Never run long-provisioning resources (API Management and similar, 30 to 60+ minutes) against real Azure from CI or from agents.** The emulator answers instantly; the scenarios assert that.

## Pages

Sorted by value to azurerm, azapi and Bicep users (ranking in [SPIKE §3](../SPIKE.md)). Counts are scenarios in tables / dropped.

| Page | Area | Engine fit | When ([PLAN](../PLAN.md)) | Scenarios |
|---|---|---|---|---|
| [API Management](api-management.md), [provider notes](api-management-provider-notes.md) | service, apis, operations, products, namedValues, backends | hooks | Slice 2 (2.4, 2.5) | 51 / 1 |
| [Authorization](authorization.md) | roleAssignments, roleDefinitions | hooks | Slice 1 (2.1 to 2.3) | 11 / 0 |
| [Management groups](management-groups.md) | managementGroups, MG-scope policy | hooks + gaps | new, after G2 | 32 / 1 |
| [Insights](insights.md) | application insights components | hooks | new, with Log Analytics | 13 / 1 |
| [Log Analytics](log-analytics.md) | workspaces | hooks | new, with Insights | 9 / 0 |
| [Event Hubs](event-hubs.md) | namespaces, eventhubs, auth rules | hooks | new, after G2 | 8 / 1 |
| [SQL](sql.md) | servers, databases, firewallRules | hooks | new (replaces stub) | 20 / 2 |
| [Service Bus](service-bus.md) | subscriptions, rules, auth rules | hooks | wave 4 | 21 / 1 |
| [Managed identity](managed-identity.md) | identities, federatedIdentityCredentials | hooks | wave 2, plus new child | 13 / 1 |
| [Redis](redis.md) | redis, firewallRules | hooks | wave 2; PUT-body fix is standalone | 9 / 1 |
| [App Configuration](app-configuration.md) | stores, soft-delete, data plane | hooks + gaps | wave 4 | 35 / 2 |
| [Container Registry](container-registry.md) | registries, credentials, replications | hooks + gaps | wave 4 | 17 / 2 |
| [Cosmos DB](cosmos-db.md) | accounts, throughput, SQL children | hooks + gaps | wave 4 | 23 / 2 |
| [Storage](storage.md) | accounts, PATCH, actions | bespoke (shell wave 4) | wave 4, optional | 16 / 1 |
| [Compute](compute.md) | availability sets, VMs | hooks / bespoke | availability sets new; VMs G5 | 8 / 1 |
| [Virtual network](virtual-network.md) | vnets, subnets, peerings | hooks | wave 4 | 12 / 0 |
| [Disk](disk.md) | disks, diskOperations | hooks + gaps | wave 2 | 10 / 0 |
| [Container Instances](container-instances.md) | containerGroups, actions | hooks + gaps | wave 4 (zero tests today) | 10 / 0 |
| [Event Grid](event-grid.md) | topics, systemTopics, subscriptions | hooks | wave 2 | 19 / 1 |
| [Key Vault](key-vault.md) | vaults; keys and certificates data plane | bespoke + hooks | wave 4; data plane never | 20 / 2 |
| [App Service](app-service.md) | serverfarms, sites | hooks / bespoke | serverfarms new; sites stay | 15 / 0 |
| [Load Balancer](load-balancer.md) | loadBalancers | passthrough | wave 4 | 3 / 0 |
| [Resource groups](resource-groups.md) | resourceGroups | bespoke, then engine | PLAN 3.3 | 8 / 0 |
| [Subscriptions and providers](subscriptions-providers.md) | subscriptions, locations, providers | engine gaps | PLAN 3.4 | 9 / 5 |
| [Deployments](deployments.md) | deployments, exportTemplate | bespoke | PLAN 3.1, 3.2 | 18 / 3 |

## Cross-cutting

| Page | Content |
|---|---|
| [Cross-cutting](cross-cutting.md) | The engine contract (`X1` to `X27`, the `armtest` rows), RG, provider, deployment and hierarchy behaviours, client smoke. 52 / 3. |
| [Client suites](client-suites.md) | Multi-client smoke pattern (Go SDK, Python SDK, `az`, PowerShell, Terraform), CA-bundle trust, CI cost classes, ordered adoption backlog. |
| [Engine lessons](engine-lessons.md) | Design lessons for the shared helpers (names, ids, errors, paging, secrets, soft-delete, actions), the porting recipe, and an ADR cross-check with **proposed** PLAN rows. Start here if you only read one page. |

## What the harvest says

- Most types are hooks plus a few shared seams. The seams not yet in an ADR are secret projection, seeded singleton children, a soft-delete path, a `NameTaken`/`ValidateLocation` helper, stateful provider registration and `$top`/`nextLink`. See the cross-check in [Engine lessons](engine-lessons.md).
- Several real PUTs are 200 or 202 only (Disk, Web API, Cosmos accounts, Storage accounts, Service Bus children). The engine's 201-on-create default needs a per-type override, and whether azurerm tolerates 201 for them is `(verify)`.
- Typical drift sources azurerm and azapi expose: handlers that drop properties they do not model, `Defaults` that regenerate GUIDs or keys per PUT, container PUTs that ignore the body (Cosmos, Redis, ACI). Each page lists the existing miniblue instances.
- Real Azure divergences worth asserting rather than copying: role-assignment duplicates are 409 `RoleAssignmentExists`; `getEntities` is POST; a missing delete is 204 or 200, not 404; a bare 202 without a poll URL hangs azurerm; `If-Match` must never be required (it breaks `terraform destroy`).
- Test cost: T1 and T2 are seconds and belong in default CI; Terraform fixtures are opt-in and batched; the API Management and Developer-tier fixtures are nightly at most.
