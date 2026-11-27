# Scope and philosophy

miniblue sits between unit tests and full integration tests. It gives you a local Azure environment that is good enough for Terraform plans, SDK smoke tests and CI pipelines without needing a real Azure subscription.

## Control plane vs data plane

Azure APIs have two layers:

- **Control plane (ARM).** Resource management via `/subscriptions/{sub}/resourceGroups/{rg}/providers/...`. Terraform, Bicep, Azure CLI and the Azure Portal use this layer to create, read, update and delete resources.
- **Data plane.** Direct operations on the resource itself, such as uploading a blob, sending a Service Bus message, or reading a Key Vault secret.

miniblue focuses on the **control plane first**. Every service starts with ARM CRUD (PUT, GET, DELETE, List) so that Terraform and infrastructure-as-code tools work. Data plane support is added for high-value developer workflows where calling a real Azure service during local development is impractical.

### What this means in practice

| Use case | miniblue support | Notes |
|----------|-----------------|-------|
| `terraform plan` and `terraform apply` | Primary focus | ARM responses match what the azurerm provider expects |
| `az group create`, `az network vnet create` | Works | ARM endpoints |
| Upload a blob, send a queue message | Works | Data plane for common dev workflows |
| Read a Key Vault secret from your app | Works | Data plane |
| Complex Cosmos DB queries with SQL syntax | Not yet | Document CRUD works, query language is on the roadmap |
| Service Bus topic subscriptions | Not yet | Basic queue send/receive works |
| Run Azure Functions locally | No | Use Azure Functions Core Tools for that |
| Azure AD/Entra ID authentication flows | Mock only | Returns valid JWT tokens but no real identity management |

## When to use miniblue

- **Local development.** Run `terraform apply` against miniblue instead of a dev subscription.
- **CI/CD pipelines.** Run miniblue as a Docker container in your pipeline to test infrastructure code.
- **SDK smoke tests.** Point your SDK client at `localhost:4566` to test CRUD operations.
- **Demos and workshops.** Run local sessions with no Azure account.

## When to use real Azure

- **Data plane edge cases.** Test against real Azure if your app relies on specific Azure behavior such as retry headers, throttling, or eventual consistency.
- **Performance testing.** miniblue is fast, and it does not simulate real Azure latency or rate limits.
- **Security testing.** miniblue accepts any credentials by design.
- **Production deployments.** miniblue is for development only.

## Design principles

1. **Start with ARM, add data plane when it matters.** A service that only has ARM CRUD is still useful for Terraform users.
2. **Satisfy clients, not byte-for-byte parity.** Responses should satisfy Terraform and SDKs rather than match Azure byte for byte.
3. **Zero config by default.** Run `docker run` with no accounts, no credentials, and no setup.
4. **One binary, one port.** One server handles every service rather than separate emulators.
5. **Real HTTP server over mock libraries.** miniblue is a running server that your tools talk to over HTTP, not a mock library.
