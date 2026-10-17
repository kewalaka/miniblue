# Client suites

> Engine fit: not applicable (test infrastructure). miniblue today: Terraform suites only (one azapi fixture, six azurerm fixtures). No Azure SDK suite, no scripted `az` suite, no PowerShell suite. Suggested wave: SDK and `az` smoke after PLAN 1.5 (armtest) so the two layers do not overlap.

Each client class exercises something an in-process httptest cannot: real credential and discovery flows, real LRO pollers, real id parsing and casing logic in the client, and the TLS trust path. This page defines the multi-client smoke pattern, the trust setup per client, how CI should wire it, and the order to build it in. The scenarios themselves are the client smoke rows on [cross-cutting scenarios](cross-cutting.md).

## Suite pattern

One small scenario per client, the same shape everywhere: create an RG, tag it, read it back, list, check existence, create one engine-served resource (a policy definition today), run one deployment, delete, assert 404. Keep each suite under the cost rule below. Breadth belongs in `tests/` httptest and armtest, not here.

| Suite | Client and runtime | Scenario | Isolation | Verdict for miniblue |
|---|---|---|---|---|
| Terraform (azurerm, azapi) | Terraform 1.16.6; azurerm `= 5.10.0`, azapi 2.13.0 | apply, plan shows no drift, second apply no-op, destroy (azurerm); apply, mangled-case GETs, destroy, 404 (azapi) | RG names inside the fixture; one fresh miniblue per fixture | Exists. Add the negative fixture, a plugin cache and a nightly latest-provider job |
| Go SDK | `armresources` (and `armpolicy` or `armauthorization`) in a separate module `tests/sdk/go` | RG create, get, list, update tags, exists, delete; policy definition CRUD through the LRO poller; ids canonical-cased; 404 after delete | Unique RG name per test | Adopt first. Only client class that exercises Go poller and id-casing logic directly |
| Python SDK | `pytest` with `azure-mgmt-resource` and `azure-mgmt-authorization` | RG, tags, deployment, policy definition via `begin_*().result()` | Unique RG name per test | Adopt second; cheap once the Go suite exists |
| `az` CLI | `mcr.microsoft.com/azure-cli:<version>` container, scripted | cloud register, login, `az group create/show/exists/list/delete`, `az policy definition create/show/delete`, `az deployment group create` | Unique RG name per run | Adopt third; feasibility spike first |
| PowerShell | pwsh 7.4 with the Az modules | same shape as `az` | Saved context restored per process | Later or optional; only if PowerShell users are a target |
| Node | `@azure/arm-*` | none planned | | Skip. No ARM suite is needed; data-plane clients are out of scope here |

Isolation today is one hard-coded subscription (the all-zeros GUID). Until a multi-subscription model exists, every test uses a unique RG name and cleans it up.

## Harness pattern

| Step | Pattern | miniblue today | Action |
|---|---|---|---|
| Start the emulator | a fresh process or image per suite, private ports, in-memory state | `tests/terraform/azurerm/run.sh` starts the binary in stub mode (`MINIBLUE_DISABLE_DOCKER=1`), on private ports, with `PERSISTENCE` and `DATABASE_URL` ignored, and refuses to run if the port is already taken | Reuse for SDK and `az` jobs. Test the shipped image in at least one job (`e2e.yml` already builds it) |
| TLS | a throwaway certificate generated per run | `LOCAL_AZURE_CERT_DIR` points the child at a temp dir; the CA bundle is built from system roots plus the run's cert | Keep generating per run. Never commit keys or certificates |
| Trust | append the cert to every CA store the client reads | `SSL_CERT_FILE` only (Go, Terraform) | Add the per-client rows in the next section |
| Endpoint discovery | clients get their endpoints from configuration, not from DNS tricks | azurerm: `ARM_METADATA_HOSTNAME=localhost:<tls port>`; azapi: provider endpoint variables | Add a `cloud.json` template for `az` and reuse it for the scripted scenario |
| Auth | a fake credential for SDKs, a service principal login for `az` and Terraform | miniblue's token endpoint returns a mock JWT for any client (`internal/services/auth/handler.go:71-95`); `ARM_CLIENT_ID`/`ARM_CLIENT_SECRET` are placeholders | A fake `TokenCredential` is enough for SDKs. Any bearer is accepted |
| Polling | skip artificial poll delays | `GO_AZURE_SDK_SKIP_POLLING_DELAY=true` in `run.sh`; the header equivalent lands with PLAN 0.8 | Keep both until the header is on every response |
| Speed | initialise providers once, reuse | `-lockfile=readonly`, `init` per fixture, no cache | Add `TF_PLUGIN_CACHE_DIR` plus an `actions/cache` step |

## Trust per client

SSL trust is where suites break first. Each client reads a different store, and several variables replace the system roots instead of adding to them.

| Client | Store it reads | What to set | Notes |
|---|---|---|---|
| Go SDK | Go system roots, or a transport you pass | `SSL_CERT_FILE` (Linux, macOS), or give the client a `Transport` with an explicit root pool | Go on Windows ignores `SSL_CERT_FILE` (documented in the acceptance guide). A custom transport works everywhere and needs no environment |
| Terraform providers (Go) | same as Go | `SSL_CERT_FILE` pointing at system roots plus the miniblue cert | The variable replaces the system roots, so build a combined bundle. `run.sh` does this for the provider registry |
| Python SDK | `requests` or `certifi` | `REQUESTS_CA_BUNDLE` or `SSL_CERT_FILE` on a combined bundle; azure-core also accepts a per-client verify path `(verify)` | A bundle must include public roots if the same process talks to anything else |
| `az` CLI | the `certifi` bundle inside the `az` Python | append the cert to the `certifi` bundle, set `REQUESTS_CA_BUNDLE`, set `AZURE_CORE_INSTANCE_DISCOVERY=false` | The OS trust store is not read. Instance discovery would otherwise try to reach the real Microsoft endpoints |
| PowerShell (.NET) | OS store; on Linux, OpenSSL paths | on Linux, `SSL_CERT_FILE` or `SSL_CERT_DIR` `(verify)`; on Windows, import into the user store | Do not rely on `Connect-AzAccount` skipping validation |
| Node | bundled roots | `NODE_EXTRA_CA_CERTS` | Adds to the bundled roots. Only relevant if a Node suite is ever built |
| `curl` (probes, helper scripts) | system bundle | `--cacert <file>` | `run.sh` pins the probe to the run's own certificate so it cannot reach another instance |

Both the Go SDK and the Python SDK refuse to send a bearer token over plain HTTP, so every SDK suite targets the TLS port (default 4567), not the HTTP port.

### Endpoint wiring per client

| Client | Wiring | miniblue endpoint it depends on |
|---|---|---|
| Go SDK | `arm.ClientOptions` with a custom `Cloud` configuration (authority host plus the ARM endpoint and audience) and a fake `TokenCredential` | `https://localhost:<tls>` for ARM; no token call is made with a fake credential |
| Python SDK | `base_url` and `credential_scopes` on the management client, plus a credential class that returns a fixed token | ARM only |
| `az` | `az cloud register --cloud-config @cloud.json` (ARM, AAD, Graph and the service suffixes), `az cloud set`, then a login | `GET /metadata/endpoints`, `GET /metadata/instance`, `POST /{tenant}/oauth2/v2.0/token`, `/oauth2/token`, the openid configuration and `/common/discovery/instance` routes. All are registered today (`internal/services/auth/handler.go:24-41`; the metadata handler serves the other two) |
| PowerShell | `Add-AzEnvironment` (ARM URL, AAD authority, suffixes) then `Connect-AzAccount`; `Save-AzContext` once so each later process restores it | same as `az` |
| Terraform | provider environment from `run.sh` (`ARM_METADATA_HOSTNAME`, `ARM_SUBSCRIPTION_ID`, `ARM_TENANT_ID`, `ARM_CLIENT_ID`, `ARM_CLIENT_SECRET`, `ARM_RESOURCE_PROVIDER_REGISTRATIONS=none`) | metadata, token, ARM |

### Docs tension

Two guides contradict these suites and need an update in the PR that proves otherwise. Do not change them speculatively.

| Page | Current claim | Consequence |
|---|---|---|
| `website/docs/guides/azure-cli.md` (line 97) | native `az` uses MSAL and cannot be reliably emulated; use `azlocal` | The `az` suite is a feasibility spike first. If it passes, rewrite the section; if not, record the failure and drop the item |
| `website/docs/guides/sdk.md` (lines 3 and 199-206) | official SDKs enforce HTTPS and real credentials, so use plain HTTP | The SDK suites show how to use them over the TLS port with a fake credential. Add a section then |
| `website/docs/guides/acceptance-tests.md` | documents `SSL_CERT_FILE` only | Add the per-client trust table above once a second client lands |

## CI wiring

Current workflows, checked in the worktree:

| Workflow | Jobs and triggers | Notes |
|---|---|---|
| `ci.yml` | vet, lint, test, build, docker; push to main and PRs to main | The test job runs `go test -v ./tests/...` (`ci.yml:47`). `internal/...` is not run and there is no race detector (PLAN 0.1) |
| `acceptance.yml` | one job, the azapi policy definition fixture; 15 minute cap; cancels superseded runs | The only azapi job. Uploads the log on failure |
| `acceptance-azurerm.yml` | matrix `azurerm / <fixture>` over `network`, `storage`, `identity-rbac`, `keyvault`, `compute`, `examples/terraform`; `fail-fast: false`; 15 minutes each | Builds the binary from source, then `ACC_SETUP_SYSTEM=1 ./tests/terraform/azurerm/run.sh <fixture>` |
| `e2e.yml` | builds the Docker image; curl and Python smoke plus the `azlocal` CLI; push, PR and manual | The only job that tests the shipped image |
| `aks-e2e.yml` | real-backend AKS run; path-filtered on AKS code, push to main and manual | The precedent for expensive, gated suites |

| Aspect | Pattern | miniblue today | Adopt |
|---|---|---|---|
| Build once, test many | one job builds the image or binary and exports it; every suite loads it | each workflow builds its own | Optional. Do it when three or more suites need the binary |
| Path filter | a changed-files filter decides which suites run; a core change runs all | `ci.yml`, `acceptance*.yml` run on every PR; only `aks-e2e.yml` and `docs.yml` filter paths | Add when the matrix passes about ten jobs. Core paths (`internal/arm`, `internal/server`, `internal/store`) always run everything |
| Client matrix | several `az` versions and provider versions, non-blocking | single pinned versions, no matrix | Add an `az` version matrix (`fail-fast: false`, non-blocking) with the `az` suite. Add a latest-provider job to the nightly |
| Nightly | a scheduled full run on unpinned versions | no `schedule` trigger in any workflow | Add one nightly workflow for drift and the slow suites |
| Manual runs | `workflow_dispatch` on expensive suites | `e2e.yml` and `aks-e2e.yml` have it; the acceptance workflows do not | Add to every new suite that misses the PR budget |
| Provider cache | cache the plugin directory | none | `TF_PLUGIN_CACHE_DIR` and `actions/cache`; serialise `init` if a cache is shared between parallel fixtures |
| Reporting | publish test results and coverage | raw logs; `acceptance.yml` uploads the log on failure | Low priority: `gotestsum --junitfile` plus a coverage upload |
| Unit tests | the same pipeline runs unit and integration tests | `internal/...` not run | PLAN 0.1 |

## Cost rule

Anything expected to take more than about 2 minutes is nightly or opt-in (`schedule` or `workflow_dispatch`), not per PR. The figures below are budget classes, not measurements; measure each suite when it lands and move it if it exceeds its class.

| Class | Budget | Runs | Suites |
|---|---|---|---|
| PR, blocking | under about 2 minutes each | every PR | `go test ./...` with `-race`, Go SDK smoke, Python SDK smoke, existing azurerm and azapi fixtures (each fixture is its own job) |
| PR, non-blocking | under about 2 minutes each | every PR, `fail-fast: false` | `az` version matrix once stable |
| Nightly | over 2 minutes | scheduled, plus manual | latest-provider drift (azurerm and azapi unpinned), PowerShell, any combined cross-service fixture, `az` against older versions |
| Opt-in | any | `workflow_dispatch` or path filter | real-backend suites (AKS style), anything needing a real cloud |

Never run any suite against real Azure. No suite needs a subscription, a login or a credit.

## Adoption backlog

Ordered. Effort: XS under half a day, S about a day, M two to four days, L engine work.

| # | Item | Effort | Depends on | Why |
|---|---|---|---|---|
| A1 | `go test ./...` with `-race` in `ci.yml` | XS | none (PLAN 0.1) | engine, store, id and deployments tests are unguarded in CI |
| A2 | Go SDK smoke in `tests/sdk/go`: own module, no `replace`, custom transport or `SSL_CERT_FILE`, fake credential | M | A1 | exercises the LRO poller and id casing in Go; Terraform's Go provider shares the same SDK base |
| A3 | Python SDK smoke: `azure-mgmt-resource`, `azure-mgmt-authorization`, `base_url`, fake credential | M | A2 | cheap once A2 exists; `begin_*().result()` covers polling |
| A4 | `az` scripted scenario plus `cloud.json` and a three-version matrix; feasibility spike first, then update `azure-cli.md` | M | A1 | the most common local-dev client; catches discovery and metadata drift |
| A5 | Negative Terraform fixture: invalid name fails apply and the message is asserted | S | a name-validation hook on at least one type | locks the `CheckName` hooks end to end |
| A6 | `TF_PLUGIN_CACHE_DIR` cache and a nightly latest-provider job | S | none | speed and early drift detection |
| A7 | Path filter on the acceptance workflows | S | matrix over about ten jobs | cost |
| A8 | Subscription-per-suite isolation | L | multi-subscription model (no PLAN row) | removes unique-name discipline |
| Later | PowerShell suite; test-result reporting; build-once artefact shared by suites; batching azapi fixtures into one job | S to M each | demand | each azapi fixture is a full CI job today |

## Pitfalls

- Go on Windows ignores `SSL_CERT_FILE`. Run the shell harness on Linux or WSL2, or pass an explicit transport in SDK tests.
- `SSL_CERT_FILE` replaces the system roots. Concatenate the public roots and the miniblue cert, or provider downloads fail.
- `az` ignores the OS trust store and needs the cert in its `certifi` bundle. Without `AZURE_CORE_INSTANCE_DISCOVERY=false` it tries the real discovery endpoints.
- SDKs refuse bearer tokens over HTTP. Point them at the TLS port.
- A shared provider plugin cache fails with "text file busy" if two `terraform init` runs share it. Serialise `init`, or copy an initialised workspace per test.
- Terraform lock files must carry hashes for every developer platform (linux_amd64, darwin_arm64, windows_amd64), or `-lockfile=readonly` fails locally.
- Pollers need terminal responses or a valid `Location`. A bare 202 stalls them; see the cross-cutting pitfalls.
- Do not copy committed key material from other projects. A per-run certificate removes the risk and the rotation chore.
- Real Azure is never a dependency, even as an oracle. Verify uncertain behaviour against the REST specs and provider source.
