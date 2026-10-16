# Terraform

miniblue works with the official `hashicorp/azurerm` Terraform provider. This guide covers setup, certificate trust, and a full example.

## Prerequisites

- miniblue running (`docker run -p 4566:4566 -p 443:4567 -p 4567:4567 moabukar/miniblue:latest`)
- Terraform 1.0+ installed
- miniblue certificate trusted (see below)

## Step 1: Trust the certificate

Terraform's azurerm provider connects to the `metadata_host` over HTTPS. You must trust miniblue's self-signed certificate.

=== "Quick (current shell)"

    ```bash
    export SSL_CERT_FILE=~/.miniblue/cert.pem
    ```

=== "Permanent (macOS)"

    ```bash
    sudo security add-trusted-cert -d -r trustRoot \
      -k /Library/Keychains/System.keychain ~/.miniblue/cert.pem
    ```

=== "Permanent (Linux)"

    ```bash
    sudo cp ~/.miniblue/cert.pem /usr/local/share/ca-certificates/miniblue.crt
    sudo update-ca-certificates
    ```

=== "Script"

    ```bash
    bash scripts/trust-cert.sh
    ```

!!! warning "Don't skip this step"
    Without certificate trust, `terraform init` will fail with a TLS handshake error when contacting the metadata endpoint.

## Step 2: Provider configuration

```hcl
terraform {
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 3.0"
    }
  }
}

provider "azurerm" {
  features {}

  # Point to miniblue HTTPS endpoint for metadata
  metadata_host = "localhost:4567"

  # Skip provider namespace registration (miniblue doesn't need it)
  skip_provider_registration = true

  # Mock credentials -- miniblue accepts anything
  subscription_id = "00000000-0000-0000-0000-000000000000"
  tenant_id       = "00000000-0000-0000-0000-000000000001"
  client_id       = "miniblue"
  client_secret   = "miniblue"
}
```

Key settings explained:

| Setting | Value | Why |
|---------|-------|-----|
| `metadata_host` | `localhost:4567` | HTTPS endpoint where Terraform fetches cloud metadata |
| `skip_provider_registration` | `true` | miniblue does not require provider namespace registration |
| `subscription_id` | Any UUID | miniblue accepts any subscription ID |
| `tenant_id` | Any UUID | miniblue accepts any tenant ID |
| `client_id` / `client_secret` | Any string | miniblue does not validate credentials |

## Step 3: Full example

This creates a resource group, virtual network, subnet, DNS zone, and container registry -- all locally.

```hcl
terraform {
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 3.0"
    }
  }
}

provider "azurerm" {
  features {}

  metadata_host              = "localhost:4567"
  skip_provider_registration = true

  subscription_id = "00000000-0000-0000-0000-000000000000"
  tenant_id       = "00000000-0000-0000-0000-000000000001"
  client_id       = "miniblue"
  client_secret   = "miniblue"
}

# --- Resource Group ---
resource "azurerm_resource_group" "example" {
  name     = "example-rg"
  location = "East US"
}

# --- Virtual Network ---
resource "azurerm_virtual_network" "example" {
  name                = "example-vnet"
  address_space       = ["10.0.0.0/16"]
  location            = azurerm_resource_group.example.location
  resource_group_name = azurerm_resource_group.example.name
}

# --- Subnet ---
resource "azurerm_subnet" "example" {
  name                 = "example-subnet"
  resource_group_name  = azurerm_resource_group.example.name
  virtual_network_name = azurerm_virtual_network.example.name
  address_prefixes     = ["10.0.1.0/24"]
}

# --- DNS Zone ---
resource "azurerm_dns_zone" "example" {
  name                = "example.local"
  resource_group_name = azurerm_resource_group.example.name
}

# --- Container Registry ---
resource "azurerm_container_registry" "example" {
  name                = "exampleregistry"
  resource_group_name = azurerm_resource_group.example.name
  location            = azurerm_resource_group.example.location
  sku                 = "Basic"
}
```

## Step 4: Apply

```bash
# Trust cert (if not done permanently)
export SSL_CERT_FILE=~/.miniblue/cert.pem

# Initialise and apply
terraform init
terraform apply -auto-approve
```

Expected output:

```
azurerm_resource_group.example: Creating...
azurerm_resource_group.example: Creation complete after 0s
azurerm_virtual_network.example: Creating...
azurerm_virtual_network.example: Creation complete after 0s
azurerm_dns_zone.example: Creating...
azurerm_dns_zone.example: Creation complete after 0s
azurerm_container_registry.example: Creating...
azurerm_container_registry.example: Creation complete after 0s
azurerm_subnet.example: Creating...
azurerm_subnet.example: Creation complete after 0s

Apply complete! Resources: 5 added, 0 changed, 0 destroyed.
```

## Step 5: Destroy

```bash
terraform destroy -auto-approve
```

All resources are removed from miniblue. Since miniblue uses in-memory storage, stopping the server also clears everything.

## Using the azapi provider

Resource types served by the [ARM engine](../architecture/arm-engine.md) (currently `Microsoft.Authorization/policyDefinitions@2023-04-01` at subscription scope) work with the `Azure/azapi` provider. The settings differ from azurerm:

```hcl
terraform {
  required_providers {
    azapi = {
      source  = "Azure/azapi"
      version = "= 2.13.0"
    }
  }
}

provider "azapi" {
  # An attribute list, not a block
  endpoint = [{
    resource_manager_endpoint       = "https://localhost:4567"
    active_directory_authority_host = "https://localhost:4567"
    resource_manager_audience       = "https://management.azure.com/"
  }]

  # Without this the Azure SDK calls an instance-discovery endpoint miniblue does not serve
  disable_instance_discovery = true
  use_cli                    = false

  subscription_id = "00000000-0000-0000-0000-000000000000"
  tenant_id       = "00000000-0000-0000-0000-000000000001"
  client_id       = "miniblue"
  client_secret   = "miniblue"
}

resource "azapi_resource" "policy" {
  type      = "Microsoft.Authorization/policyDefinitions@2023-04-01"
  name      = "deny-locations"
  parent_id = "/subscriptions/00000000-0000-0000-0000-000000000000"
  body = {
    properties = {
      policyType = "Custom"
      mode       = "All"
      policyRule = {
        if   = { not = { field = "location", in = ["westeurope"] } }
        then = { effect = "deny" }
      }
    }
  }
}
```

- azapi has no `metadata_host`, and it must use the HTTPS port (4567): the Azure SDK refuses plain HTTP credentials.
- Certificate trust is the same as above (`SSL_CERT_FILE`), with one caveat: Go on **Windows ignores `SSL_CERT_FILE`**, so on Windows add miniblue's certificate to the system store or run Terraform from WSL/Linux.
- The same settings can be supplied as environment variables: `ARM_RESOURCE_MANAGER_ENDPOINT`, `ARM_ACTIVE_DIRECTORY_AUTHORITY_HOST`, `ARM_RESOURCE_MANAGER_AUDIENCE`, `ARM_DISABLE_INSTANCE_DISCOVERY`, `ARM_USE_CLI`.
- Working fixture: `tests/terraform/azapi/policy_definition`.

## CI/CD usage

Use miniblue in your CI pipeline to test Terraform plans without an Azure account:

```yaml
# .github/workflows/terraform.yml
name: Terraform Test
on: [push]

jobs:
  test:
    runs-on: ubuntu-latest
    services:
      miniblue:
        image: moabukar/miniblue:latest
        ports:
          - 4566:4566
          - 443:4567
          - 4567:4567
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3

      - name: Trust miniblue cert
        run: |
          # Wait for miniblue to start and generate cert
          sleep 2
          docker cp $(docker ps -q --filter ancestor=moabukar/miniblue:latest):/root/.miniblue/cert.pem /tmp/miniblue.pem
          export SSL_CERT_FILE=/tmp/miniblue.pem

      - name: Terraform Init
        run: terraform init

      - name: Terraform Apply
        run: terraform apply -auto-approve
        env:
          SSL_CERT_FILE: /tmp/miniblue.pem

      - name: Terraform Destroy
        run: terraform destroy -auto-approve
        env:
          SSL_CERT_FILE: /tmp/miniblue.pem
```

## Troubleshooting

### "tls: failed to verify certificate"

The miniblue certificate is not trusted. Run:

```bash
export SSL_CERT_FILE=~/.miniblue/cert.pem
```

Or permanently trust it with `bash scripts/trust-cert.sh`.

### "connection refused" on metadata_host

Make sure miniblue is running and the HTTPS port (4567) is exposed:

```bash
curl -k https://localhost:4567/health
```

### "provider registration" errors

Ensure `skip_provider_registration = true` is set in the provider block.
