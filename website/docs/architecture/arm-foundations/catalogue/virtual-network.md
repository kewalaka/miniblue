# Virtual network

> Engine fit: hooks (virtualNetworks), passthrough child (subnets, virtualNetworkPeerings) plus a state hook (peerings), engine-gap (inline subnet sync, `privateEndpoints` side effects). miniblue today: hand-written service `internal/services/network` (vnets, subnets). Suggested wave: PLAN wave 4 for virtualNetworks and subnets; peerings as a new type after G2; `privateEndpoints` deferred.

Paths below are relative to `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Network`. Spec facts come from `Microsoft.Network/Network` `virtualNetwork.json` (checked at `2025-01-01`; later stable directories exist, the newest listed is `2026-03-01`).

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `virtualNetworks` | real latest stable `2026-03-01` `(verify)`; spec read at `2025-01-01`; azurerm pin `(verify)` | RG; list by RG and subscription | subnets, virtualNetworkPeerings | PATCH (tags only, 200); `GET {v}/CheckIPAddressAvailability?ipAddress=`; `usages` GET | hooks | `Hooks.Defaults` (stable `resourceGuid`, etag), `PatchTags`, `Under` subscription-wide list, action for `CheckIPAddressAvailability` (a GET; whether `Type.Actions` serves GET is `(verify)`) | H | S-M |
| `virtualNetworks/subnets` | same | vnet | none | `PrepareNetworkPolicies`, `UnprepareNetworkPolicies`, `ResourceNavigationLinks` (not needed) | passthrough child | child type, parent-exists (default); inline-vs-child sync has no seam | H | S |
| `virtualNetworks/virtualNetworkPeerings` | same | vnet | none | none | passthrough child + state hook | child type; `peeringState` depends on the reverse peering (cross-resource read) | H (`azurerm_virtual_network_peering`) | S-M |
| `privateEndpoints` | same | RG; list by RG and subscription | none | creates a NIC and allocates an IP | engine-gap: side effects | no seam: bespoke or a post-write hook | M | M |
| `networkSecurityGroups` | same | RG; list by RG and subscription | rules inline | PATCH (tags) | hooks | separate service `nsg`; only the PATCH delta is in this page | M | S |

## miniblue gaps

Ordered by value.

- `resourceGuid` is regenerated with `uuid.New()` on every PUT (`handler.go:173`). azurerm and azapi see drift on each apply.
- `virtualNetworkPeerings` is always `[]` (`handler.go:177`) and there are no peering routes. `azurerm_virtual_network_peering` cannot work.
- Subnet `delegations` is always `[]` (`handler.go:350`); service endpoints and policies are also not echoed, so `azurerm_subnet` delegation shows drift.
- No PATCH and no `CheckIPAddressAvailability`; no `privateEndpoints`.
- Inline `properties.subnets` on a VNet PUT deletes and recreates every subnet, but only when the key is present (`handler.go:236-237`, `handler.go:252`). Subnet PUT and DELETE sync the list back into the VNet document (`handler.go:412`, `handler.go:447`).
- DELETE of an existing vnet or subnet returns 202 with no `Location` (`handler.go:314`, `handler.go:448`); a missing one returns 404 (real: 204).
- VNet PUT does not check that the resource group exists; a missing `location` defaults to `eastus` (`handler.go:161-163`). Subnet PUT checks the vnet.
- `etag` is the constant `W/"miniblue"` (`handler.go:212`, `handler.go:388`); real etags change on each write.
- Only `vnet:` and `subnet:` are in the RG cascade list (`resourcegroups/handler.go:153-154`); peerings and private endpoints will need prefixes or `DeleteTree`.

## Scenarios

### Virtual networks

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T100 | PUT echoes the body `location`; create then update | `PUT virtualNetworks/{v}` => 201 then 200; `location` equals the request value even when it differs from the RG location | T1/T2 | Spec: 200/201. miniblue already echoes location and returns 201/200 (`handler.go:247`); keep as a regression row |
| G3-T101 | PATCH updates tags only | `PATCH` with `{"tags":{...}}` => 200, tags replaced, other properties unchanged; missing => 404 `ResourceNotFound` | T1/T2 | `PatchTags`. Spec: PATCH 200. Missing in miniblue |
| G3-T102 | PUT in a missing resource group fails | `PUT` in unknown RG => 404 `ResourceGroupNotFound` | T1/T2 | Engine default once migrated |
| G3-T103 | Delete semantics; list by RG and by subscription | `DELETE` existing => 200; missing => 204; `GET virtualNetworks` (RG) and subscription-wide list span two RGs | T1/T2 | Spec DELETE: 200/202/204. List by subscription already exists in miniblue |
| G3-T104 | Inline `properties.subnets` on the VNet PUT become subnet children | `PUT` vnet with two inline subnets => `GET virtualNetworks/{v}/subnets` lists both, each with its own `id` | T2 | Bespoke today. See the inline and child subnet sync row for the reverse direction |
| G3-T105 | `CheckIPAddressAvailability` reports availability against the VNet address space | `GET {v}/CheckIPAddressAvailability?ipAddress=` => 200; address inside the space and unallocated => `available` true; outside => false; allocated to a NIC => false; `availableIPAddresses` suggests up to 5 free addresses and skips the platform-reserved ones (first four of a subnet); a dynamic NIC address is no longer offered; a private endpoint with the same IP => 409; deleting the endpoint releases the IP; invalid IP => `available` false; unknown vnet => 404 | T2 | Real Azure checks the VNet address space, not individual subnets. Result fields: `available`, `availableIPAddresses`, `isPlatformReserved`. NIC and private-endpoint steps need the side-effect seam, so defer them `(verify)` |

### Subnets

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T106 | Subnet CRUD and list; missing parents are 404 | `PUT subnets/{s}` => 201 then 200; `GET`, `GET list`, `DELETE` (200, missing 204); missing vnet => 404; missing subnet => 404 `ResourceNotFound` | T1/T2 | Spec: PUT 200/201, DELETE 200/202/204. Generic code, not per-type codes. Parent code may be `ParentResourceNotFound` from the engine `(verify)` |
| G3-T107 | Subnet properties round-trip | `PUT` with `delegations`, `serviceEndpoints`, `privateEndpointNetworkPolicies`, `networkSecurityGroup`, `routeTable`, `addressPrefixes` => `GET` returns each as sent; `azurerm_subnet` with a delegation has an empty second plan | T2/T3 | miniblue always returns `delegations: []`. Store these as raw JSON |

### Peerings

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T108 | Peering state follows the reverse peering | `PUT virtualNetworks/{a}/virtualNetworkPeerings/{p}` with `remoteVirtualNetwork.id` and `allowVirtualNetworkAccess` => 201, `peeringState` `Initiated`; after the reverse peering exists on `{b}`, `GET` => `Connected`; list and delete work; deleting a VNet removes its peerings | T2/T3 | Spec: PUT 200/201, DELETE 200/202/204; states `Initiated`, `Connected`, `Disconnected`. State flip on delete of the remote side `(verify)`. `azurerm_virtual_network_peering`. Needs a cross-resource read |

### Private endpoints

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T109 | Private endpoint lifecycle with a NIC and IP allocation | create, get, list by RG and subscription; missing => 404; missing RG => 404; a NIC is created with a dynamic or static IP; an already-allocated IP => 409 (code `(verify)`); delete releases the IP | T2 | Not in miniblue. Engine-gap: side effects. Defer until a Terraform case needs it |

### Network security groups

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T110 | NSG lifecycle plus tag PATCH | create, get, delete, list by RG and subscription; `PATCH` tags => 200 | T2 | Covered by the `nsg` service tests; add the PATCH case there |

### Terraform

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-T111 | azurerm and azapi network fixtures | azurerm `virtual_network` (`address_space` `10.0.0.0/16`, `dns_servers`) and `subnet` (`10.0.1.0/24`) apply with an empty second plan; add azapi `virtual_network_basic` and `network_security_group_basic` | T3 | Existing azurerm case is `azurerm/network`. azapi cases are new |

### New

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G3-NET-N1 `new` | `resourceGuid` is assigned on create and stays the same | `PUT` twice and `GET` => identical `properties.resourceGuid` | T2/T3 | Drift fix; regenerated on every PUT today (`handler.go:173`) |
| G3-NET-N2 `new` | Inline and child subnets stay in sync | subnet `PUT` or `DELETE` is reflected in `GET virtualNetworks/{v}` `properties.subnets`; a vnet `PUT` with a `subnets` list replaces the set (omitted subnets are removed); a vnet `PUT` with no `subnets` key keeps existing subnets `(verify)` | T2 | The inline-vs-child sync item. azurerm re-sends subnets on vnet update; a replace must not drop nested subnet properties `(verify)` |

## Pitfalls

- Echo the request `location`, not the RG location. Create is 201 and update is 200.
- `resourceGuid` is stable across PUTs and GETs.
- `CheckIPAddressAvailability` evaluates the VNet address space, and each subnet reserves its first four addresses and the last one.
- Peering `peeringState` is `Initiated` until the reverse peering exists, then `Connected`; it is not `Connected` on first PUT.
- Not-found is the generic `ResourceNotFound` for subnets and peerings; do not mint per-type codes such as a subnet-specific one.
- Subnet fields the emulator does not model (`delegations`, `serviceEndpoints`) must still round-trip, or azurerm shows permanent drift.
- Real Azure may answer vnet writes and deletes with 202 plus a poll URL; the engine is sync (201 or 200, 200 or 204).
- A 202 with no `Location` can stall azurerm; see ADR 0006.

## Dropped

None.