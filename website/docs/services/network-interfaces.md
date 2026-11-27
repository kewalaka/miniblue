# Network Interfaces

miniblue emulates Azure Network Interfaces (NICs) via ARM endpoints. NICs support IP configurations with subnet and Public IP references, Network Security Group association, and VM back-references.

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Network/networkInterfaces/{name}` | Create or replace |
| `PATCH` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Network/networkInterfaces/{name}` | Update |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Network/networkInterfaces/{name}` | Get |
| `DELETE` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Network/networkInterfaces/{name}` | Delete |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Network/networkInterfaces` | List in resource group |
| `GET` | `/subscriptions/{sub}/providers/Microsoft.Network/networkInterfaces` | List in subscription |

## Behaviour

- Private IPv4 addresses default to deterministic values in the `10.0.x.y`
  range when `privateIPAllocationMethod` is `Dynamic` and no explicit
  `privateIPAddress` is supplied.
- miniblue assigns a stable `etag` and resource `id` on first create.
- miniblue echoes subnet, Public IP and Network Security Group references on
  each `ipConfiguration` as supplied so Terraform's reference tracking remains
  consistent.
- When a Virtual Machine references a NIC in `networkProfile.networkInterfaces`,
  miniblue sets `properties.virtualMachine.id` on the NIC to the VM resource ID.

## Terraform

```hcl
resource "azurerm_network_interface" "example" {
  name                = "myvm-nic"
  location            = azurerm_resource_group.example.location
  resource_group_name = azurerm_resource_group.example.name

  ip_configuration {
    name                          = "internal"
    subnet_id                     = azurerm_subnet.example.id
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.example.id
  }
}
```

## Limitations

- Accelerated networking is not enforced, and the flag is only stored and returned.
- Application security groups, IP tags, and auxiliary mode are not implemented.
- IPv6 configurations are accepted on `PUT` but not validated.

