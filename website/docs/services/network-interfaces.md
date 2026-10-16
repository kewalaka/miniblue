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
- A stable `etag` and resource `id` are assigned on first create.
- Subnet, Public IP and Network Security Group references on each
  `ipConfiguration` are echoed back exactly as supplied so Terraform's
  reference tracking remains consistent.
- When a NIC is referenced from a Virtual Machine's
  `networkProfile.networkInterfaces`, the VM resource ID is surfaced on the
  NIC's `properties.virtualMachine.id`.

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

- No accelerated networking enforcement (the flag is round-tripped only).
- No application security group, IP tag, or auxiliary mode behaviour.
- IPv6 configurations are accepted on PUT but not validated.

