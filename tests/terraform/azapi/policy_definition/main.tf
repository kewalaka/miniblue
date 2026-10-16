terraform {
  required_version = ">= 1.9"

  required_providers {
    azapi = {
      source  = "Azure/azapi"
      version = "= 2.13.0"
    }
  }
}

variable "miniblue_endpoint" {
  type    = string
  default = "https://localhost:4567"
}

variable "subscription_id" {
  type    = string
  default = "a1b2c3d4-0000-4000-8000-0000000000aa"
}

variable "tenant_id" {
  type    = string
  default = "a1b2c3d4-0000-4000-8000-0000000000bb"
}

variable "policy_name" {
  type    = string
  default = "Miniblue-Deny-Locations"
}

# azapi takes the endpoint as an attribute list (not a block) and needs
# instance discovery disabled; miniblue's TLS cert is trusted via SSL_CERT_FILE.
provider "azapi" {
  subscription_id            = var.subscription_id
  tenant_id                  = var.tenant_id
  client_id                  = "miniblue"
  client_secret              = "miniblue"
  use_cli                    = false
  disable_instance_discovery = true
  endpoint = [{
    resource_manager_endpoint       = var.miniblue_endpoint
    active_directory_authority_host = var.miniblue_endpoint
    resource_manager_audience       = "https://management.azure.com/"
  }]
}

resource "azapi_resource" "policy_definition" {
  type      = "Microsoft.Authorization/policyDefinitions@2023-04-01"
  parent_id = "/subscriptions/${var.subscription_id}"
  name      = var.policy_name

  body = {
    properties = {
      displayName = "Deny resources outside allowed locations"
      description = "Acceptance fixture for the miniblue ARM engine."
      policyType  = "Custom"
      mode        = "Indexed"
      metadata = {
        category = "General"
      }
      parameters = {
        allowedLocations = {
          type = "Array"
          metadata = {
            displayName = "Allowed locations"
          }
          defaultValue = ["eastus", "westeurope"]
        }
      }
      policyRule = {
        "if" = {
          not = {
            field = "location"
            in    = "[parameters('allowedLocations')]"
          }
        }
        "then" = {
          effect = "deny"
        }
      }
    }
  }
}

output "policy_definition_id" {
  value = azapi_resource.policy_definition.id
}
