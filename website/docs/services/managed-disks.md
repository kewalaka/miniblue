# Managed Disks

miniblue emulates `Microsoft.Compute/disks` so that Terraform and the Azure
SDKs can create, attach, snapshot-grant (via `beginGetAccess`) and delete
managed disks alongside virtual machines.

Disks are tracked in the in-memory (or persisted) store. miniblue also
synthesizes an OS disk record for every virtual machine created through the
[Virtual Machines](virtual-machines.md) handler so that `GET` on the VM
returns a realistic `storageProfile.osDisk.managedDisk.id` reference.

## ARM endpoints

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Compute/disks/{name}` | Create or replace |
| `PATCH` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Compute/disks/{name}` | Update |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Compute/disks/{name}` | Get |
| `DELETE` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Compute/disks/{name}` | Delete |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Compute/disks` | List in resource group |
| `GET` | `/subscriptions/{sub}/providers/Microsoft.Compute/disks` | List in subscription |
| `POST` | `.../disks/{name}/beginGetAccess` | Issue a (stub) SAS URI for read/write access |
| `POST` | `.../disks/{name}/endGetAccess` | Revoke the SAS |

`beginGetAccess` returns a deterministic placeholder SAS pointing at
`https://miniblue.local` — Terraform and the Azure SDK accept it but the
URI is not actually downloadable.

## Create a disk

```bash
curl -X PUT \
  "http://localhost:4566/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/myRG/providers/Microsoft.Compute/disks/mydisk?api-version=2024-03-02" \
  -H "Content-Type: application/json" \
  -d '{
    "location": "eastus",
    "sku": { "name": "Standard_LRS" },
    "properties": {
      "diskSizeGB": 64,
      "creationData": { "createOption": "Empty" }
    }
  }'
```

## Terraform

```hcl
resource "azurerm_managed_disk" "example" {
  name                 = "mydisk"
  location             = azurerm_resource_group.example.location
  resource_group_name  = azurerm_resource_group.example.name
  storage_account_type = "Standard_LRS"
  create_option        = "Empty"
  disk_size_gb         = 64
}
```

## Limitations

- No real block storage is allocated — `diskSizeGB`, `tier`, `iops`/`mbps`
  fields are echoed back but never enforced.
- `beginGetAccess` returns a placeholder SAS that cannot actually be used to
  upload or download disk contents.
- Snapshot and image resource types are not implemented.
