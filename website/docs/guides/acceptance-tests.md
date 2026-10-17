# Terraform acceptance tests (azurerm)

CI runs real `azurerm` (v5, pinned) against miniblue. Each fixture does:

`init` → `apply` → `plan -detailed-exitcode` (must exit 0: no drift) → second `apply` (must be a no-op) → `destroy`

This is the regression net for handler changes: if a handler returns something azurerm does not expect, the plan drifts or the apply fails.

| Fixture | Resources |
|---------|-----------|
| `network` | resource group, VNet, subnet, NSG + rule + subnet association, inline-rule NSG, public IP |
| `storage` | storage account, container (via `storage_account_id`) |
| `identity-rbac` | user-assigned identity, role assignment (RG and subscription scope) |
| `compute` | managed disk, NIC, Linux VM (stub mode, no Docker), data disk attachment |
| `keyvault` | Key Vault (ARM only) |
| `examples/terraform` | the user-facing quickstart, run in place by the same runner |

Workflow: `.github/workflows/acceptance-azurerm.yml` (matrix over fixtures, `fail-fast: false`).

## Run locally

Use Linux or WSL2. Go on Windows ignores `SSL_CERT_FILE`, so it cannot trust miniblue's certificate; the runner is a bash script for that reason.

Prerequisites: `terraform` (version in the workflow), `curl`, Go to build miniblue.

```bash
go build -o miniblue ./cmd/miniblue
tests/terraform/azurerm/run.sh network          # one fixture
tests/terraform/azurerm/run.sh examples/terraform
```

The runner starts its own miniblue (stub mode, `MINIBLUE_DISABLE_DOCKER=1`), generates a throwaway certificate, builds a CA bundle (system roots + miniblue cert) for `SSL_CERT_FILE`, and sets the `ARM_*` provider environment. Fixtures therefore only need `provider "azurerm" { features {} }`. It cleans up on exit; `ACC_KEEP=1` keeps the work dir.

| Variable | Default | Purpose |
|----------|---------|---------|
| `MINIBLUE_BIN` | `<repo>/miniblue` | miniblue binary |
| `PORT` / `TLS_PORT` | `4566` / `4567` | miniblue ports (change if taken) |
| `ACC_STEP_TIMEOUT` | `600` | seconds per terraform step |
| `ACC_SETUP_SYSTEM` | unset | allow `sudo` for `/etc/hosts` entries and `ip_unprivileged_port_start` |
| `ACC_KEEP` | unset | keep the work dir |
| `TF_PLUGIN_CACHE_DIR` | unset | avoid re-downloading the provider |

Explicit `provider "azurerm"` settings in a root module override the environment. The quickstart sets `metadata_host = "localhost:4567"`, so run it on the default ports.

### Key Vault fixture

The provider dials `https://<vault>.vault.azure.net` and waits for DNS and TLS, so the fixture needs the name in `/etc/hosts` and miniblue's TLS on `:443` (set in the fixture's `acc.env`). On a machine where you have sudo (or as root):

```bash
ACC_SETUP_SYSTEM=1 tests/terraform/azurerm/run.sh keyvault
```

This adds `127.0.0.1 acc-kv-0001.vault.azure.net` to `/etc/hosts` and lowers `net.ipv4.ip_unprivileged_port_start`. Without `ACC_SETUP_SYSTEM`, the runner stops and tells you what to do.

## Add a fixture

1. Create `tests/terraform/azurerm/<name>/main.tf` with `terraform { required_providers { azurerm = { source = "hashicorp/azurerm", version = "= 5.10.0" } } }` and `provider "azurerm" { features {} }`.
2. Create the lock file (commit it; `-lockfile=readonly` is used in CI):
   ```bash
   terraform -chdir=tests/terraform/azurerm/<name> providers lock \
     -platform=linux_amd64 -platform=darwin_arm64 -platform=windows_amd64
   ```
3. Run `tests/terraform/azurerm/run.sh <name>`.
4. Add `<name>` to the `fixture` matrix in `.github/workflows/acceptance-azurerm.yml`.

Bumping azurerm means changing the version in every fixture and refreshing every lock file.

## Known failures

No fixture fails. To record one, do not drop the fixture. Add a separate matrix entry with `continue-on-error: true` and a row here with the exact drift. One known gap has no fixture because it cannot pass yet:

| Fixture | Step | Failing diff | Tracking |
|---------|------|--------------|----------|
| none yet (`azurerm_storage_blob`, `backend "azurerm"`) | apply / init | data-plane requests to `https://<account>.blob.core.windows.net/` are not served: miniblue has no host-style blob routing | [#3](https://github.com/kewalaka/miniblue/issues/3) |

## Not covered

- Blob/queue/table/file **data plane** (`azurerm_storage_blob`, `backend "azurerm"`). The provider derives data-plane URLs from `https://<account>.blob.core.windows.net/`, which needs host-style routing in miniblue. Planned follow-up: [#3](https://github.com/kewalaka/miniblue/issues/3).
- With `MINIBLUE_STORAGE_ENDPOINT` set, `azurerm_storage_container` (and `azurerm_storage_blob` referencing a container resource) cannot work: path-style endpoints do not parse. `azurerm_storage_blob` with a literal `storage_container_id` works path-style. Leave the variable unset for Terraform; see [Configuration](../getting-started/configuration.md), including the RBAC state backend note.
- Key Vault data plane (secrets, keys).
- Docker-backed services (real containers); VMs run in stub mode.
- `examples/terraform/aks` and `examples/terraform/scenarios/*` (still azurerm `~> 3.0`).
