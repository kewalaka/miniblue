# Changelog

All notable changes to miniblue are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/). Versions follow [Semantic Versioning](https://semver.org/).

## [0.9.0](https://github.com/kewalaka/miniblue/compare/v0.8.0...v0.9.0) (2026-10-17)


### Features

* **arm:** generic ARM resource engine with policyDefinitions via azapi ([#2](https://github.com/kewalaka/miniblue/issues/2)) ([1b276fd](https://github.com/kewalaka/miniblue/commit/1b276fd5eddf3e272c95589dd9d338a7bac77e94))
* **azerr:** typed Error and skip-polling-delay header ([7599e86](https://github.com/kewalaka/miniblue/commit/7599e867b49ee54356dd5ed26c5851413213639e))
* **azerr:** typed Error with target and details ([7e67c80](https://github.com/kewalaka/miniblue/commit/7e67c80326c53226cdc1a99a215b58190a175dd1))
* merge lonegunmanb fork (ARM case-insensitivity, RBAC, KV, disks, NICs, VM fidelity) ([ab096b9](https://github.com/kewalaka/miniblue/commit/ab096b905a868ace6c99d50f9d48c561443abf82))
* **server:** send skip-polling-delay header ([6772d84](https://github.com/kewalaka/miniblue/commit/6772d84965b4fc3c1d00c84f46d2e77a2fb4eb67))
* **store:** schema version stamp with reset-required startup error ([2a8b3ec](https://github.com/kewalaka/miniblue/commit/2a8b3ec1883727da937194c4923d1758dc472528))


### Bug Fixes

* **acceptance:** isolate runner miniblue and fail fast on bind errors or non-loopback hosts ([ea4f36a](https://github.com/kewalaka/miniblue/commit/ea4f36a5ce543cd8c2b28e35a2ad71e9957fa71c))
* **acr:** return azureADAuthenticationAsArmPolicy and roleAssignmentMode ([7faf89d](https://github.com/kewalaka/miniblue/commit/7faf89ddb472d8f614977b6147874efa1f4876b4))
* **azerr:** treat typed-nil *Error as unexpected in Write ([ad00b2c](https://github.com/kewalaka/miniblue/commit/ad00b2c51f7b7c3e0efaeba32e482d5aa7eb457f))
* **blob:** persist blob content across restarts ([6dc8cca](https://github.com/kewalaka/miniblue/commit/6dc8cca2851a9ba0ba3870787863030133b58c26))
* **blob:** persist blob content across restarts ([2d02432](https://github.com/kewalaka/miniblue/commit/2d02432c8d1317ce2d206f431e7ee768478b1839))
* **compute:** return bare arrays for image lists and flatten image version properties ([8b596e8](https://github.com/kewalaka/miniblue/commit/8b596e8330666e635998efda4511387e7889d4de))
* **disks:** return 200 on PUT and tolerate deleting a missing disk ([b9ed3ea](https://github.com/kewalaka/miniblue/commit/b9ed3ea07706bb885278455bc71d5553616939f0))
* **identity:** publish a copy on PATCH instead of mutating the stored record ([f696aa1](https://github.com/kewalaka/miniblue/commit/f696aa14b27ca0df3e91a2e6f27bc10596b8eee5))
* **keyvault:** match vault name case-insensitively in RG cascade ([eae8559](https://github.com/kewalaka/miniblue/commit/eae85593990c46abd7cecdd43755dba4fc87155d))
* **keyvault:** return 200 on vault delete and add deletedVaults purge POST ([e2cec13](https://github.com/kewalaka/miniblue/commit/e2cec134df30dcf016149a7d061ba39b4262f263))
* **keyvault:** validate vault name and resource group on PUT ([d4094c9](https://github.com/kewalaka/miniblue/commit/d4094c9fa2e44014b3bb52eb18f8bff692009f4d))
* **loadbalancer:** keep resourceGuid stable across PUTs ([d23571b](https://github.com/kewalaka/miniblue/commit/d23571bd2eb52089406d0e9949806feb227c722c))
* **main:** open state before binding ports ([d287c19](https://github.com/kewalaka/miniblue/commit/d287c1987179341298a49cd8a37429a3eb82063d))
* **metadata:** use core.windows.net storage suffix unless MINIBLUE_STORAGE_ENDPOINT is set ([e104cbe](https://github.com/kewalaka/miniblue/commit/e104cbe35ef7e2ea9750aea02835ec5bd1540146))
* **nsg:** persist inline securityRules from NSG PUT ([538d042](https://github.com/kewalaka/miniblue/commit/538d0423fccb686da4ddfe45dcc896b3ab3b6d7d))
* **publicip:** echo zones and ipTags so azurerm plans clean ([898b1b6](https://github.com/kewalaka/miniblue/commit/898b1b66eb1a5560f5ca0139903ff9ec1f897be5))
* **redis:** persist location, tags and sku from PUT body ([32ffa57](https://github.com/kewalaka/miniblue/commit/32ffa57c7a92faa5d8be957ecb613a8889fbd3f1))
* **release:** drop component prefix from release-please tags ([4abf834](https://github.com/kewalaka/miniblue/commit/4abf834bbf3e0562dc719201a1c4d697f6fd4726))
* **release:** drop component prefix from release-please tags ([b3688dc](https://github.com/kewalaka/miniblue/commit/b3688dcb9b9e37f34789dcc96cfda381b08768e3))
* **server:** keep streaming, 405 and deployment dispatch working behind case-insensitive routing ([f332ca9](https://github.com/kewalaka/miniblue/commit/f332ca9de5cd78629fab25e02352b4f1b5ba8073))
* **server:** mirror case only in ARM paths and scope RBAC middleware ([f4482cc](https://github.com/kewalaka/miniblue/commit/f4482cce3dd45b2c3daa011af8eec23953da7001))
* small fidelity fixes from the spike ([860fb98](https://github.com/kewalaka/miniblue/commit/860fb981c503c78d555c076362dde37a0a9a13e1))
* **storageaccounts:** add accountMigrations/default and return 200 on container delete ([ee5254b](https://github.com/kewalaka/miniblue/commit/ee5254bb6b217425e0d1397df0a23fa79ad53be9))
* **storage:** merge service properties on PATCH and clean up account children ([4b0a57b](https://github.com/kewalaka/miniblue/commit/4b0a57b077a5e10dbd2fcd085ec717443ed0c410))
* **store:** add Decode and GetAs; fix struct-asserting call sites ([c9da10e](https://github.com/kewalaka/miniblue/commit/c9da10e8460df94e1540cf19a4c3aec236a9a0f5))
* **store:** add Decode and GetAs; fix struct-asserting call sites ([6419bab](https://github.com/kewalaka/miniblue/commit/6419bab8c25ab70a0f62e969164cb29f05f3e1a5))
* **store:** escape LIKE prefixes and add prefix index ([38211ff](https://github.com/kewalaka/miniblue/commit/38211ff065c00550f992846ba280c9620420f0f8))
* **store:** escape LIKE prefixes, add prefix index and schema stamp ([6748d07](https://github.com/kewalaka/miniblue/commit/6748d077476dbbb3a214dfad4c879065336b996f))
* **store:** keep schema stamp across reset, abort on failed read, scope key listing ([87bd186](https://github.com/kewalaka/miniblue/commit/87bd186e26f7adc5139e4a8c266af8a6d2c882a6))
* **vm:** protect existing disks, copy on write references and detach on RG delete ([65dee0e](https://github.com/kewalaka/miniblue/commit/65dee0eb81824e7215616b454bcd0f5112f1cd00))

## [0.8.0] - 2026-06-21

### Added
- **Virtual Machines (`Microsoft.Compute/virtualMachines`)**. Create/list/start/stop/restart/delete VMs via ARM API and `azlocal vm`, each backed by a real Docker container (ACI-style stub mode without Docker). Deploy multiple named services per VM (`azlocal vm deploy`), each in its own container on a per-VM Docker network with host-published ports; read per-service or combined labeled logs with tail/follow (`azlocal vm logs`); open an interactive shell (`azlocal vm ssh`) or run one-off commands (`azlocal vm run-command`, mirrors Azure runCommand). Unlike ACI, a VM create whose container fails to start fails fast with `ContainerStartFailed` and persists nothing
- **User-assigned managed identities (`Microsoft.ManagedIdentity/userAssignedIdentities`)** with `azlocal identity` CRUD. Identities can be assigned to VMs (`azlocal vm identity-assign`); workloads inside a VM (and its services) obtain attestation tokens with zero code changes via the standard `IDENTITY_ENDPOINT`/`IDENTITY_HEADER` env-var protocol that Azure SDK credential chains already probe. Tokens carry `xms_mirid` + VM claims and are verifiable via the new `POST /metadata/identity/introspect` endpoint
- README service table and comparison count bumped 27 to 28
- Website service docs for Virtual Machines, wired into the nav, services overview and the API parity matrix
- Java SDK example covering Resource Groups, Key Vault, Blob Storage and Cosmos DB (#122)

### Fixed
- Dropped an unused helper that failed `golangci-lint`, and added a `--` separator before the user-supplied image and command in the VM `docker run` invocation so a value starting with `-` cannot be parsed as a docker flag

## [0.7.0] - 2026-05-02

### Added
- **`Microsoft.Resources/deployments` endpoint Phase 1** (#118, addresses parts of #66, #74). PUT/GET/DELETE/List a deployment, walk `template.resources` in declaration order, resolve `[parameters('x')]` and `[variables('x')]`, dispatch each resource to its existing handler via the embedded chi router. Lets `az deployment group create --template-file main.bicep` work end to end against miniblue. Phase 1 does not support `[concat()]` or other template functions, copy loops, conditions, nested templates, `dependsOn` ordering, outputs evaluation, or non-RG-scope deployments
- **Helm / Kubernetes deployment guide** at `/guides/helm` (#117, closes #86) covering `helm install`, persistence, PostgreSQL backend via secret, exposing the service, and the real-backend limitations in-cluster
- **`SHA256SUMS` release asset** (#115, closes #94). The release workflow now publishes a single checksums file alongside the binaries

### Changed
- README comparison table service count bumped 26 to 27 (AKS shipped in v0.6.0)
- CHANGELOG backfilled for v0.4.2 through v0.6.0 from each release's notes (#116)

### CI
- New AKS pipeline job that exercises the `:full` Docker image with a mounted docker socket, locking in the in-container real-backend path against future Dockerfile or alpine bumps (#114)

## [0.6.0] - 2026-05-01

### Added
- **Azure Kubernetes Service (AKS) emulation** (#111, closes #50). `Microsoft.ContainerService/managedClusters` with two backends: stub (default, ARM only) and real (rancher/k3s in Docker, opt-in via `AKS_BACKEND=k3s`). `azlocal aks create/list/show/delete/get-credentials` commands. `get-credentials` merges into `~/.kube/config` like `az aks` does
- **`full` Dockerfile target** that adds the docker CLI so AKS and ACI real backends work in-container with the host docker socket mounted
- **Dedicated AKS CI pipeline** (`aks-e2e.yml`): real-backend k3s smoke (DinD on the runner) and a Terraform apply against the AKS example
- Resource group cascade delete now cleans up child AKS resources

### Changed
- `cmd/miniblue` graceful shutdown now propagates to AKS so k3s containers do not leak past miniblue's lifetime
- 27 services total (was 26)

## [0.5.1] - 2026-05-01

### Fixed
- Container startup crash on fresh Docker runs. Scratch image runs as `USER 65534` but had no writable home directory. Now ships with `/home/nonroot` owned by 65534 and `HOME` set, so the cert dir can be created cleanly (#110)
- Cert load when existing certs are unusable: missing, corrupted, or expired files now fall through to regeneration instead of `log.Fatal`. Thanks @KarasAlison (#109)
- Bump `github.com/go-sql-driver/mysql` to v1.10.0 (#108)

### Changed
- E2E now builds the image from the PR's `Dockerfile` instead of pulling `:latest` from Docker Hub, so regressions are caught on the PR that introduces them. Failed startups dump `docker logs miniblue` for diagnostics (#110)
- Bump `mislav/bump-homebrew-formula-action` to v4 (#107)

## [0.5.0] - 2026-04-21

### Added
- **App Service & Web Apps** (`Microsoft.Web/sites`) with full CRUD, slots, publishing credentials, and config sub-resources (app settings, auth settings v1/v2, connection strings, backup, logs, metadata, push settings, slot config names, storage accounts) (#103, contributed by @abusarah-tech)
- **App Service Plans** (`Microsoft.Web/serverFarms`) with SKU tier detection
- **Container Apps** (`Microsoft.App/containerApps`) with CRUD, start/stop, revisions, secrets, auth tokens, custom domain analysis, and subscription-level listing
- **Managed Environments** (`Microsoft.App/managedEnvironments`) with CRUD and app logs configuration
- **Container App Jobs** (`Microsoft.App/jobs`) with CRUD, start/stop, executions, detectors, and secrets
- `CheckNameAvailability` endpoint for sites
- New `containerapp` and `containerapp env` commands in `azlocal`
- New `containerapps` and `webapps` Terraform scenarios

### Changed
- Existing Azure Functions emulation refactored into the broader `sites` package

### Fixed
- Improved error handling when loading existing TLS certificates and keys (#105, fixes #104)

## [0.4.4] - 2026-04-16

### Added
- Persistent volume support in the Helm chart (#83). Survives pod restarts when enabled in values.yaml
- PostgreSQL backend support in the Helm chart via Kubernetes secret (`databaseUrlSecret.name` / `.key`)

### Changed
- Secure defaults set on the Helm container, matching the Dockerfile's non-root user (#88)

## [0.4.3] - 2026-04-16

### Fixed
- Store backend data race in `FileBackend.Save()`: auto-save goroutine could read inconsistent state while handlers were writing. Now uses an atomic `Snapshot()` under a single lock (#84, #89)
- `PostgresBackend.List()` and `ListByPrefix()` returned `nil` on error instead of empty slices, causing inconsistent behavior with the memory backend
- `azlocal` CLI no longer silently swallows `json.Marshal` and `io.ReadAll` errors in `doPut`/`doPost`/`printResponse`. Errors now go to stderr (#85)

### Changed
- E2E workflow now runs on every push to main and every PR (was `workflow_dispatch` only) so breaking changes can not slip through unnoticed (#82)
- CI example bumped to `actions/checkout@v6` (#91)

## [0.4.2] - 2026-04-15

### Fixed
- `miniblue --version` now reports the correct version instead of `dev`. Version is injected via ldflags in all build targets: release binaries, Docker images, and Homebrew formula

## [0.4.1] - 2026-04-15

### Fixed
- Resource tags now preserved and returned on all ARM services (VNets, NSGs, Public IPs, Load Balancers, App Gateways, DNS Zones, Storage Accounts). Fixes constant Terraform drift
- Resource group cascade delete now cleans up all 26 child resource types (was missing NSGs, Public IPs, Load Balancers, App Gateways, Storage Accounts, Cosmos DB, Service Bus, App Config)
- Subscription-level list endpoints added for VNets, NSGs, Public IPs, Load Balancers, App Gateways and DNS Zones. Fixes Terraform data source 404s
- Renamed `_arm_test.go` files that were silently skipped on amd64/arm64 (34 tests now run)
- Added 3 missing providers to registration list (PostgreSQL, Redis, Container Instances)
- Added 10 missing service documentation pages to website

### Changed
- PR template updated with all 26 services
- Added @abusarah-tech to CODEOWNERS
- Added public ROADMAP.md

## [0.3.0] - 2026-04-14

### Added
- Public IP Addresses service (`Microsoft.Network/publicIPAddresses`) with full CRUD, static/dynamic allocation
- Network Security Groups service (`Microsoft.Network/networkSecurityGroups`) with security rules and cascade delete
- Azure Load Balancer service (`Microsoft.Network/loadBalancers`) with frontend IPs, backend pools, rules and probes
- Application Gateway service (`Microsoft.Network/applicationGateways`) with full L7 config
- Azure Storage Accounts ARM service with shared key auth (contributed by @abusarah-tech)
- Terraform examples for load balancer and application gateway
- Terraform example for storage accounts
- .NET example using HttpClient (contributed by @sa-es-ir)
- E2E test suite for storage accounts
- `azlocal` CLI commands for storage account operations

### Changed
- Service count: 21 to 26
- Blob handler refactored to work under storage accounts

## [0.2.0] - 2026-04-04

### Added
- Azure Database for PostgreSQL (real DB creation via `POSTGRES_URL`)
- Azure Database for MySQL (ARM management API)
- Azure SQL Database (ARM management API)
- Azure Cache for Redis (real connectivity via `REDIS_URL`)
- Azure Container Instances (real Docker containers when available)
- Structured JSON logging (method, path, status, latency)
- `/metrics` endpoint (uptime, request count, error rate)
- PostgreSQL persistent storage backend via `DATABASE_URL`
- Per-service test files (37 tests across 16 files)
- Pre-commit hooks for code quality
- Terraform scenario examples (three-tier, serverless, microservices)
- CHANGELOG.md
- CODEOWNERS
- RELEASING.md with step-by-step release guide
- mkdocs documentation site (17 pages)

### Changed
- Go version bumped to 1.25
- Dockerfile now runs as non-root user (`miniblue`)
- Dockerfile includes HEALTHCHECK
- golangci-lint added to CI pipeline
- Service count: 14 to 19

### Fixed
- Terraform destroy flow (async polling + list resources endpoint)
- VNet/Subnet/ACR responses include all fields the azurerm provider expects
- DNS zones auto-create SOA and NS records
- Metadata trailing slashes removed (prevented double-slash 404s)
- All ARM curl examples include `?api-version=` parameter

## [0.1.0] - 2026-04-04

### Added
- Initial release with 14 Azure services
- Resource Groups, Blob Storage, Table Storage, Queue Storage
- Key Vault, Cosmos DB, Service Bus, Azure Functions
- Virtual Networks, DNS Zones, Container Registry, Event Grid
- App Configuration, Managed Identity
- Terraform azurerm provider support (5 resource types)
- `azlocal` CLI (like awslocal for LocalStack)
- Self-signed TLS certificate management
- Multi-arch Docker images (amd64 + arm64)
- GHCR and DockerHub publishing
- Mock OAuth2/OIDC authentication
- ARM API versioning middleware
- Azure-compatible error responses

[0.4.1]: https://github.com/moabukar/miniblue/compare/v0.4.0...v0.4.1
[0.3.0]: https://github.com/moabukar/miniblue/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/moabukar/miniblue/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/moabukar/miniblue/releases/tag/v0.1.0
