# Key Vault

miniblue emulates Azure Key Vault vault resources and secret management. Create vaults through ARM-compatible `Microsoft.KeyVault/vaults` endpoints, then create, read, list, and delete secrets without an Azure subscription.

## API endpoints

### ARM resource endpoints

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.KeyVault/vaults/{name}` | Create or replace a vault |
| `PATCH` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.KeyVault/vaults/{name}` | Update vault tags/properties |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.KeyVault/vaults/{name}` | Get a vault |
| `DELETE` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.KeyVault/vaults/{name}` | Delete a vault and place it in the deleted-vault store when soft delete is enabled |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.KeyVault/vaults` | List vaults in a resource group |
| `GET` | `/subscriptions/{sub}/providers/Microsoft.KeyVault/vaults` | List vaults in a subscription |
| `POST` | `/subscriptions/{sub}/providers/Microsoft.KeyVault/checkNameAvailability` | Check vault name availability |
| `PUT` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.KeyVault/vaults/{name}/accessPolicies/{add\|remove\|replace}` | Update vault access policies |
| `GET` | `/subscriptions/{sub}/providers/Microsoft.KeyVault/locations/{location}/deletedVaults/{name}` | Get a soft-deleted vault |
| `DELETE` | `/subscriptions/{sub}/providers/Microsoft.KeyVault/locations/{location}/deletedVaults/{name}` | Purge a soft-deleted vault |

Vault create/delete responses include `Azure-AsyncOperation`, `Location`, and `Retry-After` headers and complete synchronously; polling the operation URL returns `{"status":"Succeeded"}`.

Vault resources include Azure-compatible fields such as `properties.tenantId`, `properties.sku`, `properties.accessPolicies`, `properties.vaultUri`, `properties.enableSoftDelete`, `properties.softDeleteRetentionInDays`, `properties.enableRbacAuthorization`, `properties.publicNetworkAccess`, `properties.networkAcls`, and `properties.provisioningState`.

### Data-plane secret endpoints

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/keyvault/{vault}/secrets/{name}` | Set a secret |
| `GET` | `/keyvault/{vault}/secrets/{name}` | Get a secret |
| `DELETE` | `/keyvault/{vault}/secrets/{name}` | Delete a secret |
| `GET` | `/keyvault/{vault}/secrets` | List all secrets |
| `PUT` | `https://{vault}.vault.azure.net/secrets/{name}` | Azure SDK/Terraform data-plane set |
| `GET` | `https://{vault}.vault.azure.net/secrets/{name}[/{version}]` | Azure SDK/Terraform data-plane get |
| `DELETE` | `https://{vault}.vault.azure.net/secrets/{name}` | Azure SDK/Terraform data-plane delete |
| `GET`/`DELETE` | `https://{vault}.vault.azure.net/deletedsecrets/{name}` | Soft-delete lookup/purge |

The HTTPS listener certificate includes `*.vault.azure.net` for lab DNS/hosts-file routing.

## Create a vault

```bash
curl -X PUT \
  "http://localhost:4566/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/myRG/providers/Microsoft.KeyVault/vaults/myvault?api-version=2023-07-01" \
  -H "Content-Type: application/json" \
  -d '{
    "location": "eastus",
    "properties": {
      "tenantId": "00000000-0000-0000-0000-000000000000",
      "sku": { "family": "A", "name": "standard" },
      "accessPolicies": [],
      "enableSoftDelete": true
    }
  }'
```

Response:

```json
{
  "id": "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/myRG/providers/Microsoft.KeyVault/vaults/myvault",
  "name": "myvault",
  "type": "Microsoft.KeyVault/vaults",
  "location": "eastus",
  "properties": {
    "tenantId": "00000000-0000-0000-0000-000000000000",
    "sku": { "family": "A", "name": "standard" },
    "accessPolicies": [],
    "vaultUri": "https://myvault.vault.azure.net/",
    "provisioningState": "Succeeded"
  }
}
```

## Set a secret

```bash
curl -X PUT "http://localhost:4566/keyvault/myvault/secrets/db-password" \
  -H "Content-Type: application/json" \
  -d '{"value": "P@ssw0rd123!"}'
```

Response:

```json
{
  "id": "https://myvault.vault.azure.net/secrets/db-password/1870b9f2c1f",
  "value": "P@ssw0rd123!",
  "attributes": {
    "created": 1778930000,
    "enabled": true,
    "updated": 1778930000
  }
}
```

Setting the same secret name again overwrites the value.

## Get a secret

```bash
curl "http://localhost:4566/keyvault/myvault/secrets/db-password"
```

Returns the same JSON structure as above. Returns `404` if the secret does not exist.

## List all secrets in a vault

```bash
curl "http://localhost:4566/keyvault/myvault/secrets"
```

```json
{
  "value": [
    {
      "id": "https://myvault.vault.azure.net/secrets/db-password/1870b9f2c1f",
      "attributes": {
        "created": 1778930000,
        "enabled": true,
        "updated": 1778930000
      }
    },
    {
      "id": "https://myvault.vault.azure.net/secrets/api-key/1870b9f2c20",
      "attributes": {
        "created": 1778930001,
        "enabled": true,
        "updated": 1778930001
      }
    }
  ]
}
```

List responses return metadata only; secret values are intentionally omitted, matching Azure Key Vault behavior.

## Delete a secret

```bash
curl -X DELETE "http://localhost:4566/keyvault/myvault/secrets/db-password"
```

Response: `200 OK`

## Multiple vaults

Vaults are separated by name. For ARM/Terraform/Azure CLI parity, create a vault resource first. The lightweight data-plane endpoints still scope secrets to whatever vault name you use in the URL for local curl workflows.

```bash
# Different vaults, same secret name
curl -X PUT "http://localhost:4566/keyvault/prod-vault/secrets/api-key" \
  -H "Content-Type: application/json" \
  -d '{"value": "prod-key-123"}'

curl -X PUT "http://localhost:4566/keyvault/dev-vault/secrets/api-key" \
  -H "Content-Type: application/json" \
  -d '{"value": "dev-key-456"}'
```

## azlocal

```bash
# Vault resource CRUD
azlocal keyvault vault create --resource-group myRG --name myvault --location eastus
azlocal keyvault vault list --resource-group myRG
azlocal keyvault vault show --resource-group myRG --name myvault
azlocal keyvault vault delete --resource-group myRG --name myvault

# Set
azlocal keyvault secret set --vault myvault --name db-password --value "P@ssw0rd123!"

# Get
azlocal keyvault secret show --vault myvault --name db-password

# List
azlocal keyvault secret list --vault myvault

# Delete
azlocal keyvault secret delete --vault myvault --name db-password
```

## Terraform

The ARM vault endpoints are designed for `azurerm_key_vault`, `azurerm_key_vault_secret`, and `azurerm_role_assignment` workflows. RBAC assignments are stored by the `Microsoft.Authorization` emulator; miniblue does not enforce access control on Key Vault data-plane requests.

## Full example

```bash
#!/bin/bash
set -e

VAULT="app-vault"

# Store application secrets
curl -X PUT "http://localhost:4566/keyvault/${VAULT}/secrets/db-host" \
  -H "Content-Type: application/json" \
  -d '{"value": "localhost"}'

curl -X PUT "http://localhost:4566/keyvault/${VAULT}/secrets/db-password" \
  -H "Content-Type: application/json" \
  -d '{"value": "supersecret"}'

curl -X PUT "http://localhost:4566/keyvault/${VAULT}/secrets/jwt-signing-key" \
  -H "Content-Type: application/json" \
  -d '{"value": "my-signing-key-256"}'

# List all secrets
curl -s "http://localhost:4566/keyvault/${VAULT}/secrets" | jq '.value[].id'

# Retrieve one
DB_PASS=$(curl -s "http://localhost:4566/keyvault/${VAULT}/secrets/db-password" | jq -r '.value')
echo "DB password: ${DB_PASS}"
```
