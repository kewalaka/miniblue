# Terraform Scenarios

Example architectures you can test locally with miniblue.

> **Note:** these scenarios (and `../aks`) still pin azurerm `~> 3.0` and are not covered by CI. The CI-gated azurerm v5 example is [`../`](../). To use a scenario with azurerm v4+, replace `skip_provider_registration = true` with `resource_provider_registrations = "none"` in its `provider.tf`.

## Scenarios

| Scenario | Description | Resources |
|----------|-------------|-----------|
| [three-tier](three-tier/) | Web + App + Data tier with VNet isolation | RG, VNet, 3 Subnets, DNS, ACR |
| [serverless](serverless/) | Event-driven with Functions and Event Grid | RG, Event Grid, DNS |
| [microservices](microservices/) | Multi-service with shared infra and per-service subnets | 4 RGs, VNet, 3 Subnets, ACR, DNS, Event Grid |
| [containerapps](containerapps/) | Container Apps with Managed Environments and Jobs | RG, Managed Environment, 2 Container Apps, Container App Job |
| [webapps](webapps/) | App Service with Linux Web App | RG, App Service Plan, Linux Web App |

## Running any scenario

```bash
# 1. Start miniblue
./bin/miniblue

# 2. Trust the cert
export SSL_CERT_FILE=~/.miniblue/cert.pem

# 3. Run the scenario
cd examples/terraform/scenarios/three-tier
terraform init
terraform apply -auto-approve

# 4. Verify with azlocal
azlocal group list
azlocal health

# 5. Tear down
terraform destroy -auto-approve
```
