# Managed Identity

miniblue covers two parts of Azure managed identity:

1. The **Instance Metadata Service (IMDS)** token endpoint, used by Azure
   SDKs running "inside" a VM/container to obtain an OAuth2 access token.
2. **User-assigned managed identities** (`Microsoft.ManagedIdentity/userAssignedIdentities`),
   used by Terraform and the Azure CLI to model identities as standalone ARM
   resources that can be referenced from role assignments and VMs.

## IMDS endpoints (data plane)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/metadata/identity/oauth2/token` | Returns a mock bearer token (24h expiry). Accepts the standard `resource=` query parameter. |
| `GET` | `/metadata/instance` | Returns a static instance metadata document. |

```bash
curl "http://localhost:4566/metadata/identity/oauth2/token?resource=https://management.azure.com/" \
  -H "Metadata: true"
```

The token is a mock JWT — it is accepted by miniblue's other services but
will not validate against real Azure.

## User-assigned identity ARM endpoints

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.ManagedIdentity/userAssignedIdentities/{name}` | Create or replace |
| `PATCH` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.ManagedIdentity/userAssignedIdentities/{name}` | Update tags |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.ManagedIdentity/userAssignedIdentities/{name}` | Get |
| `DELETE` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.ManagedIdentity/userAssignedIdentities/{name}` | Delete |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.ManagedIdentity/userAssignedIdentities` | List in resource group |

`principalId`, `clientId` and `tenantId` are deterministically derived from
the identity's resource ID so they remain stable across restarts.

## Create a user-assigned identity

```bash
curl -X PUT \
  "http://localhost:4566/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/myRG/providers/Microsoft.ManagedIdentity/userAssignedIdentities/myid?api-version=2023-01-31" \
  -H "Content-Type: application/json" \
  -d '{ "location": "eastus", "tags": { "env": "dev" } }'
```

## Terraform

```hcl
resource "azurerm_user_assigned_identity" "example" {
  name                = "myid"
  location            = azurerm_resource_group.example.location
  resource_group_name = azurerm_resource_group.example.name
}

resource "azurerm_role_assignment" "kv" {
  scope                = azurerm_key_vault.example.id
  role_definition_name = "Key Vault Secrets User"
  principal_id         = azurerm_user_assigned_identity.example.principal_id
}
```

## azlocal

```bash
azlocal identity create --resource-group myRG --name myid --location eastus
azlocal identity list   --resource-group myRG
azlocal identity show   --resource-group myRG --name myid
azlocal identity update --resource-group myRG --name myid --tags "env=prod"
azlocal identity delete --resource-group myRG --name myid
```

## Limitations

- The IMDS token is a static mock — it carries no real claims and is not
  signed.
- Federated identity credentials (`federatedIdentityCredentials`) are not
  implemented.
- System-assigned identities on VMs and other resources are echoed back on
  PUT/GET but not modelled as separate resources.
