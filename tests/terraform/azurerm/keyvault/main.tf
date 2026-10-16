terraform {
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "= 5.10.0"
    }
  }
}

provider "azurerm" {
  features {}
}

data "azurerm_client_config" "current" {}

resource "azurerm_resource_group" "this" {
  name     = "acc-keyvault-rg"
  location = "eastus"
}

# ARM only. The provider waits for the vault's DNS name to resolve and answer on 443,
# so run.sh needs "127.0.0.1 acc-kv-0001.vault.azure.net" in /etc/hosts and miniblue on :443.
resource "azurerm_key_vault" "this" {
  name                       = "acc-kv-0001"
  location                   = azurerm_resource_group.this.location
  resource_group_name        = azurerm_resource_group.this.name
  tenant_id                  = data.azurerm_client_config.current.tenant_id
  sku_name                   = "standard"
  rbac_authorization_enabled = true
  purge_protection_enabled   = false
  tags                       = { env = "acc" }
}
