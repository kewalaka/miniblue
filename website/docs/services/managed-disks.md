# Managed Disks

miniblue emulates `Microsoft.Compute/disks` so Terraform and the Azure SDKs
can create, attach, grant access to via `beginGetAccess`, and delete managed
disks alongside virtual machines.

miniblue tracks disks in its in-memory or persisted store. When you create a
virtual machine through the [Virtual Machines](virtual-machines.md) handler
without an existing managed disk, miniblue creates an OS disk with `managedBy`
set to the VM. A `GET` request on the VM then returns a valid
`storageProfile.osDisk.managedDisk.id` reference, and a `GET` request on the
disk resolves.

## ARM endpoints

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Compute/disks/{name}` | Create or replace |
| `PATCH` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Compute/disks/{name}` | Update |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Compute/disks/{name}` | Get |
| `DELETE` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Compute/disks/{name}` | Delete |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Compute/disks` | List in resource group |
| `GET` | `/subscriptions/{sub}/providers/Microsoft.Compute/disks` | List in subscription |
| `POST` | `.../disks/{name}/beginGetAccess` | Issue a stub SAS URI for read/write access |
| `POST` | `.../disks/{name}/endGetAccess` | Revoke the SAS |

`beginGetAccess` returns a deterministic placeholder SAS URI pointing at
`https://miniblue.local`. Terraform and the Azure SDK accept it, but the URI
is not downloadable.

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

- miniblue does not allocate block storage. It echoes back `diskSizeGB`, `tier`,
  `iops`, and `mbps` fields without enforcing them.
- `beginGetAccess` returns a placeholder SAS URI that cannot upload or download
  disk contents.
- Snapshot and image resource types are not implemented.
