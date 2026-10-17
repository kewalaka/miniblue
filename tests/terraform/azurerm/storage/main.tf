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

resource "azurerm_resource_group" "this" {
  name     = "acc-storage-rg"
  location = "eastus"
}

resource "azurerm_storage_account" "this" {
  name                     = "accstorage0001"
  resource_group_name      = azurerm_resource_group.this.name
  location                 = azurerm_resource_group.this.location
  account_tier             = "Standard"
  account_replication_type = "LRS"
  tags                     = { env = "acc" }
}

# Created through ARM (storage_account_id), so it needs no blob data plane.
# azurerm_storage_blob and the azurerm backend are not covered: they need host-style
# <account>.blob.core.windows.net routing, which miniblue does not implement yet.
resource "azurerm_storage_container" "this" {
  name                  = "acc-container"
  storage_account_id    = azurerm_storage_account.this.id
  container_access_type = "private"
}
