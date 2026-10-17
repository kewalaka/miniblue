# Load Balancer

> Engine fit: passthrough (all `Microsoft.Network/loadBalancers` types). miniblue today: hand-written service `internal/services/loadbalancer`, already a superset of what the engine needs (full CRUD, three child types, list by subscription). Suggested wave: wave 4 (PLAN phase 4). Until then only the small additions below are worth doing.

miniblue is ahead of the source inventory here. The source scenarios are mostly covered by `tests/loadbalancer_test.go` (lifecycle, not found, defaults, multiple, and per-child lifecycle, defaults and not found for pools, probes and rules). This page records the remainder.

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.Network/loadBalancers` | up to 2026-03-01 in the Network spec dir; tests use 2023-09-01 | RG; list by subscription | `backendAddressPools`, `probes`, `loadBalancingRules` (all present); `frontendIPConfigurations` (read-only, derived from properties), `inboundNatRules`, `inboundNatPools`, `outboundRules` (not present) | none in scope | passthrough | `PatchMode` tags (real PATCH is a tags update `(verify)`), `ListSubscription` (PLAN 1.2), `Request.Current` for stable `resourceGuid`, RG existence | L | S |
| `loadBalancers/{backendAddressPools,probes,loadBalancingRules}` | same | child | none | none | passthrough | parent existence (`ParentResourceNotFound`) | L | S |

## miniblue gaps

Ordered by value. Only the first group is worth doing before the wave 4 migration.

- No PATCH on a load balancer (`internal/services/loadbalancer/handler.go:23-57`, route table). Real PATCH updates tags only.
- `resourceGuid` is regenerated on every PUT (`handler.go:156`; children at `handler.go:188`, `handler.go:219`, `handler.go:238`). It must stay stable for the life of the resource.
- DELETE returns a bare 202 with no operation headers (`handler.go:311`). A client that polls the LRO has nothing to follow. Sync-only (ADR-0006) wants 200 when it existed and 204 when it did not. The existing `TestLoadBalancerDeleteNotFound` asserts 404 for a missing load balancer and will flip to 204.
- Default location is `eastus` (`handler.go:131-134`) instead of requiring `location` or taking it from the resource group.
- Default `sku` is `Standard` / `Regional` when none is sent (`handler.go:136-139`); real Azure applies `Basic` when the field is omitted on older versions `(verify)`.
- No RG-exists check on PUT.
- Inline arrays on the load balancer body (`backendAddressPools`, `probes`, `loadBalancingRules`) and the child routes are not kept in sync, so a rule added through the child route is invisible in the parent `GET`.
- No azurerm or azapi fixture for a load balancer (`tests/terraform/azurerm/network/main.tf` has none).

## Scenarios

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T106 | Create, get, delete, list by RG, list by subscription, update tags. | `PUT` => 201 on create, 200 on replace; `GET` => 200; `PATCH` `{tags}` => 200 and tags persist on the next `GET`; list by subscription includes every load balancer; `DELETE` => 200 then `GET` => 404 `ResourceNotFound`. | T2 | Extend `tests/loadbalancer_test.go`. New work is PATCH and the DELETE status. List by subscription already exists (`handler.go:57`). |
| G2-T107 | azurerm and azapi create a basic-SKU load balancer. | Apply and destroy succeed; sku `Basic`; tags preserved across a no-op re-apply. | T3 | New fixture; none exists today. Which child reads azurerm issues after create is `(verify)`. |
| G2-T108 | Show returns every property. | `GET` body carries `frontendIPConfigurations`, `backendAddressPools`, `loadBalancingRules`, `probes` and `inboundNatRules` as configured, with stable ids and `resourceGuid`. | T2 | Needs the inline and child sync, plus `inboundNatRules` support (worth adding is `(verify)`). |

## Pitfalls

- A real PATCH on a load balancer changes tags only. Treat any other body field as ignored or rejected `(verify)`; do not run a generic deep merge.
- `resourceGuid` is assigned once. Regenerating on PUT breaks diff checks in azurerm.
- A bare 202 with no `Location` or `Azure-AsyncOperation` header is a polling trap; return 200 or 204 synchronously.
- Real DELETE on a missing load balancer returns 204 in ARM, not 404.
- PUT on an existing load balancer returns 200, not 201.
- A child with a missing parent load balancer is 404 `ParentResourceNotFound`, not `ResourceNotFound`.
- A missing or malformed api-version is 400; any well-formed version is accepted.

## Dropped

| ID | Reason |
|---|---|
| none | Nothing dropped; every id in this group is a scenario above. |
