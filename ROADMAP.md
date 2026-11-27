# Roadmap

This is the public roadmap for miniblue. It outlines what we are working on and what is planned.

Priorities shift based on community feedback. Open an issue or discussion if something here matters to you.

## Now (in progress)

- **Improve Terraform parity.** Fill gaps in existing service responses so more `azurerm` resources work out of the box.
- **Azure SDK compatibility.** Test and fix responses for the official Azure SDKs (Go, Python, .NET, JavaScript).
- **Better error messages.** Return the exact error codes and formats that Azure returns so client libraries handle errors accurately.
- **Document control plane vs data plane scope.** Clarify what miniblue emulates and where the boundary sits.

## Next (planned)

### New services

| Service | Provider | Priority | Notes |
|---------|----------|----------|-------|
| Azure Kubernetes Service (AKS) | `Microsoft.ContainerService` | High | ARM management only, no real cluster |
| App Service / Web Apps | `Microsoft.Web` | High | Extends existing Functions stub |
| Management Groups | `Microsoft.Management` | Medium | Hierarchy, subscription assignment |
| Azure Monitor | `Microsoft.Insights` | Medium | Metrics and diagnostic settings |
| Traffic Manager | `Microsoft.Network` | Medium | DNS-based routing profiles |
| Azure Front Door | `Microsoft.Cdn` | Medium | CDN and WAF policies |
| Private DNS Zones | `Microsoft.Network` | Medium | Extends existing DNS service |
| Network Interfaces (NIC) | `Microsoft.Network` | Medium | Required for VM provisioning |
| Azure Policy | `Microsoft.Authorization` | Medium | Policy definitions, assignments and evaluation on deployments |
| Managed Disks | `Microsoft.Compute` | Low | Disk resources for VMs |
| Virtual Machines | `Microsoft.Compute` | Low | ARM management only |

### Platform improvements

- **Resource tags.** Store and return tags on all ARM resources, and support tag filtering on list operations.
- **Bicep and ARM template deployment.** Accept `Microsoft.Resources/deployments`, process ARM template JSON, and support what-if operations. This enables `az deployment group create --template-file main.bicep` locally.
- **Policy evaluation.** Evaluate Azure Policy on resource create and update operations and return deny responses. This enables testing deployments against policy in regulated environments without a real Azure subscription.
- **Persistent state across restarts.** Improve file and PostgreSQL backends for local environments that keep state between runs.
- **Webhooks and event delivery.** Add Event Grid subscriptions that call HTTP endpoints.
- **Service Bus subscriptions and dead-letter queues.** Add topic subscriptions and dead-letter queue handling.
- **Key Vault keys and certificates.** Add key and certificate endpoints alongside secrets.
- **Cosmos DB query language.** Add basic SQL query support.
- **Multi-tenancy.** Support multiple tenants, subscriptions, and management groups, and allow creating and switching between them for multi-account testing.

## Later (exploratory)

- Azure Active Directory / Entra ID (beyond mock tokens)
- Azure RBAC and role assignments
- Cosmos DB Mongo and Cassandra APIs
- Azure Batch
- Azure SignalR
- Logic Apps

## Scope

miniblue primarily targets the ARM control plane (resource CRUD via Terraform, Bicep, and Azure CLI) with selective data plane support for common developer workflows (blob upload, queue send, secret get).

- The ARM control plane is the primary focus with broad service coverage.
- The data plane covers common developer workflows.
- Run integration tests against real Azure for data plane edge cases.
- miniblue sits between unit tests with mocks and full integration tests against real Azure.

## Non-goals

These are out of scope:

- **Full Azure API compatibility.** miniblue returns responses that satisfy Terraform, SDKs, and common workflows rather than 1:1 API parity.
- **Production workloads.** miniblue is for local development and CI/CD testing.
- **Authentication enforcement.** miniblue accepts any credentials by design.
- **Multi-region replication.** All data is local, and regions are cosmetic.

## How to contribute

Pick an item from this roadmap and open a pull request. See [CONTRIBUTING.md](CONTRIBUTING.md) for the full guide. Each service follows the same handler pattern; look at any file in `internal/services/` for a template.

Open an issue before starting work so we can coordinate and avoid duplicate effort.
