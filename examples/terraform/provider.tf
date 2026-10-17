provider "azurerm" {
  features {}

  # Point to miniblue metadata endpoint (HTTPS)
  metadata_host = "localhost:4567"

  # miniblue does not implement provider registration
  resource_provider_registrations = "none"

  # Mock credentials - miniblue accepts anything
  subscription_id = "00000000-0000-0000-0000-000000000000"
  tenant_id       = "00000000-0000-0000-0000-000000000001"
  client_id       = "miniblue"
  client_secret   = "miniblue"
}
