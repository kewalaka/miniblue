# miniblue Terraform example
#
# Start miniblue first:
#   ./bin/miniblue
#
# Trust the cert (Linux / WSL2; keep the system roots in the bundle):
#   cat /etc/ssl/certs/ca-certificates.crt ~/.miniblue/cert.pem > ~/.miniblue/ca-bundle.pem
#   export SSL_CERT_FILE=~/.miniblue/ca-bundle.pem
#
# Run:
#   terraform init
#   terraform apply -auto-approve
#
# Test with azlocal after apply:
#   azlocal group list
#   azlocal network vnet show --name example-vnet --resource-group example-rg
#   azlocal dns zone show --name example.local --resource-group example-rg
#   azlocal acr show --name exampleregistry --resource-group example-rg
#
# Destroy:
#   terraform destroy -auto-approve

resource "azurerm_resource_group" "example" {
  name     = "example-rg"
  location = "East US"
}
