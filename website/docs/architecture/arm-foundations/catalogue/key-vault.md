# Key Vault

> Engine fit is a hand-written control plane today (vaults, access policies, deleted vaults) that may move to hooks plus `CheckName` as an optional ARM shell. The keys and certificates data plane is bespoke and is the largest data-plane gap; secrets stay on chi. miniblue today has a hand-written service `internal/services/keyvault` covering vaults, accessPolicies, deletedVaults, and a secrets-only data plane. Suggested wave is wave 4 for the ARM shell (optional, PLAN phase 4). Keys and certificates are not an engine job because data planes never migrate (ADR-0007).

The control plane is already close to complete. The scenarios below mostly tighten it (name validation, resource group check, delete status codes, recover mode) and then add the two missing data planes. Keys and certificates are the large work. They are standalone chi routes backed by Go `crypto/*` and `crypto/x509`, with no ARM involvement.

## Types

| Resource type | api-versions seen (real latest `(verify)`) | Scopes | Children | Non-CRUD ops | Engine fit | Needs (ADR seams) | Value | Effort |
|---|---|---|---|---|---|---|---|---|
| `Microsoft.KeyVault/vaults` | 2026-05-15 latest stable in the spec dir; also 2026-02-01, 2025-05-01, 2024-11-01, 2023-07-01, 2023-02-01 | RG; list by subscription | accessPolicies, keys and secrets (management-plane views), privateEndpointConnections | `PUT` with `createMode=recover` | hand-written (hooks if migrated) | `Hooks.Validate` (name), `Defaults` (soft delete, retention), RG existence, `PatchMode` merge, `Provisioned` | M | S |
| `vaults/accessPolicies/{add,remove,replace}` | same | child of vaults | none | `PUT` per operation kind | hand-written (action-like) | `Type.Actions` or keep hand-written | M | S |
| `Microsoft.KeyVault/locations/{l}/deletedVaults` | same | subscription | none | `GET`, list, `POST purge` | hand-written | none | M | S |
| `POST .../providers/Microsoft.KeyVault/checkNameAvailability` | same | subscription | none | not applicable | provider-level chi route | `CheckName` metadata (ADR-0005) | M | S |
| `vaults/keys`, `vaults/secrets` (management plane) | same | child of vaults | none | `PUT`, `GET`, list | not scoped | none | L | S |
| Secrets data plane `https://{vault}.vault.azure.net/secrets` | 7.x `(verify)` | vault host | versions, deletedsecrets | recover, purge, backup, restore | bespoke (chi) | none | M | M |
| Keys data plane `.../keys` | 7.x `(verify)` | vault host | versions, rotationpolicy, deletedkeys | create, import, encrypt, decrypt, wrapkey, unwrapkey, `rng`, `release` | bespoke (chi) | none | H | L |
| Certificates data plane `.../certificates` | 7.x `(verify)` | vault host | versions, pending, policy, contacts, issuers | create, import, merge | bespoke (chi) | none | M | L |

Spec facts used below are confirmed in the 2026-05-15 management-plane spec. Create requires `location` and `properties`, and `properties` requires `tenantId` and `sku`. `enableSoftDelete` defaults to true and cannot be turned off. `softDeleteRetentionInDays` is 7 to 90 with a default of 90. `createMode` is `recover` or `default`. `checkNameAvailability` takes `name` and `type` (both required) and its `reason` is `AccountNameInvalid` or `AlreadyExists`. `PUT` on a vault lists 200 and 201, and `DELETE` lists 200 and 204.

## miniblue gaps

Ordered by value.

- Keys data plane is absent. There are no create, get, list, versions, delete, recover, purge, rotation policy, `rng`, encrypt, decrypt, wrap or unwrap routes (`internal/services/keyvault/handler.go:33-74` is the route table).
- Certificates data plane is absent (create, import, pending, merge, versions, policy).
- Secrets data plane is partial. Set, get, get by version, list, delete, and deletedsecrets get and purge exist. Missing routes are list versions, update attributes, backup and restore, recover, and deleted list (`handler.go:33-74`).
- Secrets have a persistence bug. Secrets are stored as a Go struct and read back with a type assertion (`handler.go:574`, `handler.go:654`). After a restart or on a Postgres store, the assertion fails and data is dropped. Store plain JSON-shaped maps instead.
- Vault PUT does no name validation and no resource group existence check (`CreateOrUpdateVault`, `handler.go:280-302`). `vaultNamePattern` (`handler.go:456`) and `validVaultName` (`handler.go:458`, which also rejects `--`) are only used by `CheckNameAvailability` (`handler.go:462`).
- There is no `createMode=recover` on PUT, although deleted-vault list, get and purge exist. Purge accepts `DELETE` returning 204 (`handler.go:523`) and `POST .../purge` returning 202 (`handler.go:530`). Sync-only (ADR-0006) expects 200 for the POST form.
- Async headers are faked. `setOperationHeaders` (`handler.go:273`) and the 200-with-headers DELETE (`handler.go:336`) emulate an LRO. Sync-only drops them.
- DELETE of a missing vault returns 404 (`handler.go:336`). The spec lists 200 or 204, so 204 is expected `(verify)`.
- Vault PATCH exists (`handler.go:304`), as do access policy updates (`handler.go:390`, add, remove, replace; invalid operation returns 400) and subscription list (`handler.go:380`).
- Existing coverage includes `tests/keyvault_test.go` (`TestKeyVaultCRUD`, `TestKeyVaultDataPlaneVaultAzureNetHostCRUD`, `TestKeyVaultARMVaultCRUDAndAccessPolicies`) and an azurerm fixture at `tests/terraform/azurerm/keyvault/main.tf`. There are no keys or certificates tests.

## Scenarios

### Vaults (control plane)

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T85 | Vault names are 3 to 24 characters, start with a letter, end with a letter or digit, contain only letters, digits and hyphens, and have no consecutive hyphens. | `PUT vaults/{n}` with 2 characters, a leading digit, a trailing `-`, `--`, more than 24 characters, or an illegal character returns 400 `VaultNameNotValid` `(verify)`; `a-b1` returns 201. | T2 | `Hooks.Validate`. The error code is `(verify)`. |
| G2-T86 | Name availability. | `POST checkNameAvailability` with `{name, type}` returns 200. An existing vault returns `nameAvailable:false` and `reason:AlreadyExists`; an invalid name returns false with `AccountNameInvalid`; a soft-deleted name returns false with `AlreadyExists` `(verify)`; a free name returns true. | T2 | Partly covered today; add the soft-deleted case. Uses `CheckName` metadata. |
| G2-T87 | Create in a missing resource group. | `PUT` returns 404 `ResourceGroupNotFound` `(verify)`. | T1 | Engine RG existence check. |
| G2-T88 | Create with each security option and tags. | `PUT` returns 201; `GET` reflects `enabledForDeployment`, `enabledForDiskEncryption`, `enabledForTemplateDeployment`, `enableRbacAuthorization`, premium `sku`, `softDeleteRetentionInDays`, and tags. Missing `tenantId` or `sku` returns 400 `(verify)`. | T2 | Retention cannot be changed after create `(verify)`. Defaults are soft delete on and retention 90. |
| G2-T89 | Get, list and delete. | `GET` returns 200; a missing vault returns 404 `ResourceNotFound`; list by RG and by subscription include all vaults; `DELETE` existing returns 200; `DELETE` missing returns 204 `(verify)`. | T1 | The spec lists 200 or 204 for delete, never 404. |
| G2-T90 | Update each property with PATCH. | `PATCH vaults/{n}` returns 200; each property from T88 changes and others remain unchanged. | T2 | `PatchMerge`. Array handling for `accessPolicies` under PATCH is `(verify)`. |
| G2-T91 | Deleted vault lifecycle. | After `DELETE`, `GET locations/{l}/deletedVaults/{n}` returns 200 and list includes it; `POST .../purge` returns 200 and removes the entry; `PUT` with `createMode=recover` returns 200 or 201 and makes the vault readable again. | T2 | CLI `wait` is moot in sync-only. Purge answers 202 today (`handler.go:530`). |
| G2-T92 | Access policy operations. | `PUT vaults/{n}/accessPolicies/add` with an `objectId` returns 200 and the policy appears in `GET vaults/{n}`; `.../remove` returns 200 and removes it. | T2 | `replace` is the third kind. An unknown kind returns 400 `(verify)`. |
| G2-T103 | azurerm and azapi create a vault and an RSA key. | Apply and destroy succeed; `soft_delete_retention_days` is reflected; the RSA key is created through the data plane. | T3 | The key case needs the keys data plane (T94). Extend the existing fixture and add an azapi variant. |
| G2-T105 | Full SDK flow for vault, secret, key, and certificate. | All four create and read back; key and certificate data-plane calls use the vault host. | T4 | Composite of T88, T93, T94 and T101. Implement last. |

### Data plane (bespoke)

#### Secrets

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T93 | Deleted secrets. | `GET /deletedsecrets` returns 200 with `value: []` when empty and all deleted names otherwise; `POST /deletedsecrets/{n}/recover` returns 200, restores `GET /secrets/{n}`, and removes the name from the deleted list; `DELETE /deletedsecrets/{n}` (purge) returns 204 and removes it. | T2 bespoke | List and recover are missing today. |
| G2-N12 | `new`. Secret versions and attribute updates. | A second `PUT /secrets/{n}` creates a new version; `GET /secrets/{n}/versions` lists both; `PATCH /secrets/{n}/{version}` updates `attributes` without creating a version. | T2 bespoke | `new`. Also fixes the persistence shape (`handler.go:574`). |

#### Keys

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T94 | Create RSA and EC keys. | `POST /keys/{n}/create` with `{kty: RSA}` or `{kty: EC, crv}` returns 200 with a bundle containing `key.kid`, `kty`, `n` and `e` (RSA) or `crv`, `x`, `y` (EC); a requested `key_size` is honoured. | T2 bespoke | Uses Go `crypto/rsa` and `crypto/ecdsa`. Supported sizes and curves are `(verify)`. |
| G2-T95 | Delete then recover a key. | `DELETE /keys/{n}` returns 200 with `recoveryId`; `POST /deletedkeys/{n}/recover` returns 200; `GET /keys/{n}` returns 200; the name is absent from `GET /deletedkeys` after recovery. | T2 bespoke | A deleted key reads as 404 `KeyNotFound` `(verify)`. |
| G2-T96 | Rotation policy round trip. | `PUT /keys/{n}/rotationpolicy` with `lifetimeActions` and `attributes.expiryTime` returns 200; `GET` returns the same policy. | T2 bespoke | Default policy when none is set is `(verify)`. |
| G2-T97 | Random bytes. | `POST /rng` with `{count}` returns 200; `value` is base64url of `count` bytes. | T2 bespoke | Real support may be limited to Managed HSM `(verify)`. Do not build without confirming. |
| G2-T98 | Encrypt and decrypt. | `POST /keys/{n}/{v}/encrypt` and `.../decrypt` with `RSA-OAEP-256` and `RSA1_5` return 200; decrypting the ciphertext returns the original. | T2 bespoke | Payloads are base64url without padding. The version segment may be optional `(verify)`. |
| G2-T99 | Wrap and unwrap. | `POST .../wrapkey` and `.../unwrapkey` with `RSA-OAEP-256` return 200; a round trip returns the original key bytes. | T2 bespoke | |

#### Certificates

| ID | Behaviour (real Azure) | Expect | Tier | Notes |
|---|---|---|---|---|
| G2-T101 | Create or import a certificate. | `POST /certificates/{n}/create` returns 202 with an in-progress operation; `GET /certificates/{n}/pending` returns status then `completed`; `GET /certificates/{n}` returns 200 with `id`, `kid`, `sid`, `x5t`, and `cer`; versions list; pending can be deleted; merge completes an externally signed request. | T2 bespoke | Uses Go `crypto/x509` self-signed certificates. Create answering 202 is `(verify)`. |
| G2-T102 | azurerm `azurerm_key_vault_certificate`. | Apply and destroy succeed; `secret_id` contains the name; `thumbprint` and `version` are non-empty. | T3 | Needs T101. New fixture. |

## Pitfalls

- A DELETE on a missing resource is never 404 in ARM. Return 204.
- PUT on a vault returns 201 on create and 200 on replace. The spec marks create as long-running; sync-only returns the final state with no async headers.
- Vault names are globally unique because they form the host `{name}.vault.azure.net`, and soft-deleted names still count as taken.
- `enableSoftDelete` cannot be turned off, and `enablePurgeProtection` cannot be turned off once on. Real Azure leaves purge protection unset unless requested, while miniblue writes `false` (`handler.go:218`) `(verify)`.
- Do not persist data-plane objects as Go structs; round trips through a JSON-backed store lose them.
- Key and certificate payloads use base64url without padding.
- Real Azure does not return a name-check `reason` outside `AccountNameInvalid` and `AlreadyExists`.
- A data-plane host is the vault host, not the ARM host. Tests must resolve `{name}.vault.azure.net` to miniblue.
- Access policy changes go through `PUT .../accessPolicies/{add|remove|replace}` (200 or 201 in the spec). No other verb is defined for them.

## Dropped

| ID | Reason |
|---|---|
| G2-T100 | Key `release` returns an attestation-bound token. It needs an attestation service and is out of scope. |
| G2-T104 | Data-plane authorization (RBAC versus access policy enforcement) is out of scope because miniblue has no authorization enforcement and the Authorizer seam is metadata only. |
