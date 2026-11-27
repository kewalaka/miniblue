# Load Balancer

> Engine fit is passthrough for all `Microsoft.Network/loadBalancers` types. miniblue today has a hand-written service `internal/services/loadbalancer` that is already a superset of what the engine needs (full CRUD, three child types, list by subscription). Suggested wave is wave 4 (PLAN phase 4). Until then only the small additions below are worth doing.

miniblue is ahead of the source inventory here. `tests/loadbalancer_test.go` covers most of the source scenarios (lifecycle, not found, defaults, multiple load balancers, and per-child lifecycle, defaults and not found for pools, probes and rules). This page records the remainder.

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.Network/loadBalancers` | up to 2026-03-01 in the Network spec dir; tests use 2023-09-01 | RG; list by subscription | `backendAddressPools`, `probes`, `loadBalancingRules` (all present); `frontendIPConfigurations` (read-only, derived from properties), `inboundNatRules`, `inboundNatPools`, `outboundRules` (not present) | none in scope | passthrough | `PatchMode` tags (real PATCH is a tags update `(verify)`), `ListSubscription` (PLAN 1.2), `Request.Current` for stable `resourceGuid`, RG existence | L | S |
| `loadBalancers/{backendAddressPools,probes,loadBalancingRules}` | same | child | none | none | passthrough | parent existence (`ParentResourceNotFound`) | L | S |

## miniblue gaps

Ordered by value. Only the first group is worth doing before the wave 4 migration.

- No PATCH route exists on a load balancer (`internal/services/loadbalancer/handler.go:23-57`). Real PATCH updates tags only.
- `resourceGuid` is regenerated on every PUT (`handler.go:156`; children at `handler.go:188`, `handler.go:219`, `handler.go:238`). It must stay stable for the life of the resource.
- DELETE returns a bare 202 with no operation headers (`handler.go:311`), so a client that polls the LRO has nothing to follow. Sync-only (ADR-0006) expects 200 when it existed and 204 when it did not. The existing `TestLoadBalancerDeleteNotFound` asserts 404 for a missing load balancer and will flip to 204.
- Default location is `eastus` (`handler.go:131-134`) instead of requiring `location` or taking it from the resource group.
- Default `sku` is `Standard` / `Regional` when none is sent (`handler.go:136-139`). Real Azure applies `Basic` when the field is omitted on older versions `(verify)`.
- PUT does not check that the resource group exists.
- Inline arrays on the load balancer body (`backendAddressPools`, `probes`, `loadBalancingRules`) and the child routes are not kept in sync, so a rule added through the child route is invisible in the parent `GET`.
- No azurerm or azapi fixture exists for a load balancer (`tests/terraform/azurerm/network/main.tf` has none).

## Scenarios

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T106 | Create, get, delete, list by RG, list by subscription, and update tags. | `PUT` returns 201 on create and 200 on replace; `GET` returns 200; `PATCH` with `{tags}` returns 200 and persists tags on the next `GET`; list by subscription includes every load balancer; `DELETE` returns 200 and `GET` after returns 404 `ResourceNotFound`. | T2 | Extend `tests/loadbalancer_test.go`. New work is PATCH and the DELETE status. List by subscription already exists (`handler.go:57`). |
| G2-T107 | azurerm and azapi create a basic-SKU load balancer. | Apply and destroy succeed; sku is `Basic`; tags are preserved across a no-op re-apply. | T3 | New fixture because none exists today. Which child reads azurerm issues after create is `(verify)`. |
| G2-T108 | Show returns every property. | `GET` body carries `frontendIPConfigurations`, `backendAddressPools`, `loadBalancingRules`, `probes` and `inboundNatRules` as configured, with stable ids and `resourceGuid`. | T2 | Needs inline and child sync, plus `inboundNatRules` support (whether it is worth adding is `(verify)`). |

## Pitfalls

- A real PATCH on a load balancer changes tags only. Treat any other body field as ignored or rejected `(verify)`; do not run a generic deep merge.
- `resourceGuid` is assigned once. Regenerating it on PUT breaks diff checks in azurerm.
- A bare 202 with no `Location` or `Azure-AsyncOperation` header is a polling trap. Return 200 or 204 synchronously.
- Real DELETE on a missing load balancer returns 204 in ARM, not 404.
- PUT on an existing load balancer returns 200, not 201.
- A child with a missing parent load balancer returns 404 `ParentResourceNotFound`, not `ResourceNotFound`.
- A missing or malformed api-version returns 400; any well-formed version is accepted.

## Dropped

| ID | Reason |
|---|---|
| none | Nothing is dropped; every id in this group is a scenario above. |
