# Terraform + miniblue

Use miniblue as a local Azure emulator with Terraform (azurerm `5.10.0`).

This example is run by CI on every PR (`tests/terraform/azurerm/run.sh examples/terraform`: apply, no-drift plan, no-op apply, destroy), so it stays in step with the provider and miniblue. The lock file pins the provider.

## Quick Start

```bash
# 1. Start miniblue
./bin/miniblue

# 2. Trust the self-signed certificate (one-time)
#    Option A: System-wide (recommended)
bash scripts/trust-cert.sh

#    Option B: Session only (Linux / WSL2). SSL_CERT_FILE replaces the system roots,
#    so keep them in the bundle or `terraform init` cannot reach the registry.
cat /etc/ssl/certs/ca-certificates.crt ~/.miniblue/cert.pem > ~/.miniblue/ca-bundle.pem
export SSL_CERT_FILE=~/.miniblue/ca-bundle.pem

# 3. Run Terraform
cd examples/terraform
terraform init
terraform plan
terraform apply -auto-approve
```

## How it works

The `azurerm` provider has a `metadata_host` argument. When set, it fetches
`/metadata/endpoints?api-version=2022-09-01` over HTTPS and uses the returned
`resourceManagerEndpoint` for all ARM API calls.

miniblue serves this on port 4567 (HTTPS with a self-signed cert) and
returns `http://localhost:4566` as the resource manager endpoint.

## Certificate Trust

The `azurerm` provider always uses HTTPS for `metadata_host`. miniblue
generates a self-signed certificate on first run and saves it to
`~/.miniblue/cert.pem`.

You must trust this certificate. Options:

| Method | Command | Scope |
|--------|---------|-------|
| Script | `bash scripts/trust-cert.sh` | System-wide (persistent) |
| macOS | `sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain ~/.miniblue/cert.pem` | System-wide |
| Linux | `sudo cp ~/.miniblue/cert.pem /usr/local/share/ca-certificates/miniblue.crt && sudo update-ca-certificates` | System-wide |
| Windows | `certutil -user -addstore Root "$env:USERPROFILE\.miniblue\cert.pem"` | Current user |
| Env var (Linux / WSL2) | combined bundle, see Quick Start | Current session only |

Go on Windows and macOS ignores `SSL_CERT_FILE`: use the system store there, or run Terraform from WSL2/Linux.

## Authentication

miniblue accepts any credentials. The mock values in `provider.tf` are
placeholders - no real Azure auth needed.

## Notes

- `metadata_host` takes hostname:port only (no https:// prefix)
- The cert is reused across restarts (regenerated if expiring within 24h)
- `resource_provider_registrations = "none"` prevents namespace registration calls (`skip_provider_registration` was removed in azurerm v4)
- `LOCAL_AZURE_CERT_DIR` env var overrides cert storage location
- Storage: `azurerm_storage_container` works with `storage_account_id`; `azurerm_storage_blob` and `backend "azurerm"` need host-style data-plane endpoints, a planned follow-up ([#3](https://github.com/kewalaka/miniblue/issues/3)). Do not set `MINIBLUE_STORAGE_ENDPOINT` with Terraform.
