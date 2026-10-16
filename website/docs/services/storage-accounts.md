# Storage Accounts

miniblue emulates Azure Storage Accounts via ARM endpoints with shared key authentication. Storage accounts provide the management layer on top of blob, queue, table and file storage.

## API endpoints

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Storage/storageAccounts/{name}` | Create or update |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Storage/storageAccounts/{name}` | Get |
| `DELETE` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Storage/storageAccounts/{name}` | Delete |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Storage/storageAccounts` | List in resource group |
| `GET` | `/subscriptions/{sub}/providers/Microsoft.Storage/storageAccounts` | List in subscription |
| `POST` | `.../storageAccounts/{name}/listKeys` | List access keys |
| `GET`/`PUT`/`PATCH` | `.../storageAccounts/{name}/{blob,file,queue,table}Services/default` | Per-service properties |
| `GET`/`PUT`/`DELETE` | `.../storageAccounts/{name}/blobServices/default/containers/{containerName}` | ARM-style container CRUD |
| `PUT`/`GET`/`DELETE` | `.../storageAccounts/{name}/managementPolicies/default` | Lifecycle management policy |

## Create a storage account

```bash
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Storage/storageAccounts/mystorageacct?api-version=2023-05-01" \
  -H "Content-Type: application/json" \
  -d '{
    "location": "eastus",
    "kind": "StorageV2",
    "sku": {"name": "Standard_LRS"}
  }'
```

## Terraform

```hcl
resource "azurerm_storage_account" "example" {
  name                     = "examplestorage"
  resource_group_name      = azurerm_resource_group.example.name
  location                 = azurerm_resource_group.example.location
  account_tier             = "Standard"
  account_replication_type = "LRS"
}
```

## Shared key authentication

miniblue supports Azure shared key authentication for storage data plane operations. Keys are auto-generated when a storage account is created.

For local scripting where SharedKey signing is impractical, set
`MINIBLUE_DISABLE_SHAREDKEY_AUTH=1` to bypass signature verification on the
blob data plane. This is **only** for development workflows — never enable
it in an environment that pretends to model production.

## Lifecycle management policies

miniblue accepts and round-trips Azure storage lifecycle management policies
via the `managementPolicies/default` sub-resource. The rule bodies are stored
verbatim but are **not enforced** against blob data.

```bash
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Storage/storageAccounts/mystorageacct/managementPolicies/default?api-version=2023-05-01" \
  -H "Content-Type: application/json" \
  -d '{
    "properties": {
      "policy": {
        "rules": [{
          "enabled": true,
          "name": "delete-old-logs",
          "type": "Lifecycle",
          "definition": {
            "actions": { "baseBlob": { "delete": { "daysAfterModificationGreaterThan": 30 } } },
            "filters": { "blobTypes": ["blockBlob"], "prefixMatch": ["logs/"] }
          }
        }]
      }
    }
  }'
```

azlocal mirrors the same shape:

```bash
azlocal storage account management-policy create --name mystorageacct \
  --resource-group myRG --policy @policy.json
azlocal storage account management-policy show   --name mystorageacct --resource-group myRG
azlocal storage account management-policy delete --name mystorageacct --resource-group myRG
```

## Limitations

- No private endpoints or firewall rules
- No geo-replication
- Management policies are stored but not enforced
- File storage data plane is not yet implemented
