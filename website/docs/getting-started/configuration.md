# Configuration

miniblue works with zero configuration out of the box. This page covers everything you can customise.

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `4566` | HTTP server port |
| `TLS_PORT` | `4567` | HTTPS server port (self-signed certificate) |
| `LOG_LEVEL` | `info` | Log verbosity: `debug`, `info`, `warn`, `error` |
| `LOCAL_AZURE_ENDPOINT` | `http://localhost:4566` | Endpoint used by the `azlocal` CLI |
| `LOCAL_AZURE_CERT_DIR` | `~/.miniblue` | Directory for TLS certificate and key |
| `DATABASE_URL` | _(unset)_ | PostgreSQL connection string for persistent storage |
| `MINIBLUE_STORAGE_ENDPOINT` | _(unset)_ | Overrides storage account data-plane endpoints to point to local server (enables SDK/data-plane testing) |
| `MINIBLUE_DISABLE_SHAREDKEY_AUTH` | _(unset)_ | When truthy (`1`, `true`, `yes`, `on`), skips **all** blob data-plane authentication (SharedKey and Bearer); tokens are never validated. Dev-only — never enable in environments that model production. |
| `MINIBLUE_DISABLE_DOCKER` | _(unset)_ | When set, forces [Virtual Machines](../services/virtual-machines.md) into stub mode even if a Docker daemon is reachable. |

When set, storage accounts return local **path-style** endpoints (e.g., `http://localhost:4566/blob/{accountName}/`) instead of real Azure endpoints (e.g., `https://{accountName}.blob.core.windows.net/`). This allows SDKs and tools to connect directly to miniblue's local blob/queue/table/file services for data-plane testing.

!!! warning "Terraform and `MINIBLUE_STORAGE_ENDPOINT`"
    With this variable set, `azurerm_storage_container` cannot work, and neither can `azurerm_storage_blob` when its `storage_container_id` references an `azurerm_storage_container`: the provider cannot parse path-style endpoints. `azurerm_storage_blob` with a literal `storage_container_id` (an ARM container ID string) does work path-style. To use `azurerm_storage_container`, leave the variable unset: it then works through the ARM API (use `storage_account_id`). Host-style data-plane endpoints (`https://{accountName}.blob.core.windows.net/`) served by miniblue are a planned follow-up ([#3](https://github.com/kewalaka/miniblue/issues/3)).

!!! note "State backend with RBAC today"
    `backend "azurerm"` works against miniblue with Entra auth when `use_azuread_auth = true`, `lookup_blob_endpoint = true` and `resource_group_name` are set (no `access_key`/`sas_token`), and miniblue runs with `MINIBLUE_STORAGE_ENDPOINT` (for example `http://localhost:4566`) and `MINIBLUE_DISABLE_SHAREDKEY_AUTH=1`. The backend then takes the path-style endpoint from the ARM response.

    `MINIBLUE_DISABLE_SHAREDKEY_AUTH` is a blunt, dev-only switch: it skips **all** blob data-plane authentication (SharedKey and Bearer), and tokens are never validated. Without it, the blob handler returns 403 `AuthenticationFailed` for Bearer requests on accounts created through ARM. The proper fix is tracked in [#3](https://github.com/kewalaka/miniblue/issues/3).

The metadata endpoint's `suffixes.storage` is `core.windows.net` by default, and `localhost:<PORT>` when `MINIBLUE_STORAGE_ENDPOINT` is set.

Example -- run on custom ports:

```bash
PORT=8080 TLS_PORT=8443 ./bin/miniblue
```

## Ports

| Port | Protocol | Used by |
|------|----------|---------|
| **4566** | HTTP | curl, SDKs, Terraform `endpoint`, azlocal, REST calls |
| **4567** | HTTPS | Terraform `metadata_host`, Azure CLI (MSAL), auth endpoints |

!!! note "Why two ports?"
    The HTTP port (4566) handles all resource management APIs. The HTTPS port (4567) is required because Terraform's `azurerm` provider fetches cloud metadata over HTTPS, and the Azure CLI (MSAL) requires TLS for auth endpoints. If you only use curl or azlocal, you only need port 4566.

## TLS certificate

On first start, miniblue generates a self-signed certificate at:

```
~/.miniblue/cert.pem
~/.miniblue/key.pem
```

The certificate is valid for 1 year. miniblue reuses an existing certificate if it has more than 24 hours until expiry.

### Trusting the certificate

You need to trust the certificate for Terraform, Go SDKs, and other tools that verify TLS.

=== "Quick (Linux / WSL2, current session)"

    ```bash
    cat /etc/ssl/certs/ca-certificates.crt ~/.miniblue/cert.pem > ~/.miniblue/ca-bundle.pem
    export SSL_CERT_FILE=~/.miniblue/ca-bundle.pem
    ```

    `SSL_CERT_FILE` replaces Go's system roots, so the bundle keeps them (otherwise `terraform init` cannot reach the provider registry).

=== "Windows (permanent)"

    ```powershell
    certutil -user -addstore Root "$env:USERPROFILE\.miniblue\cert.pem"
    ```

    Go on Windows ignores `SSL_CERT_FILE`; it uses the Windows certificate store. Alternatively run Terraform from WSL2.

=== "macOS (permanent)"

    ```bash
    sudo security add-trusted-cert -d -r trustRoot \
      -k /Library/Keychains/System.keychain ~/.miniblue/cert.pem
    ```

=== "Debian / Ubuntu (permanent)"

    ```bash
    sudo cp ~/.miniblue/cert.pem /usr/local/share/ca-certificates/miniblue.crt
    sudo update-ca-certificates
    ```

=== "RHEL / Fedora (permanent)"

    ```bash
    sudo cp ~/.miniblue/cert.pem /etc/pki/ca-trust/source/anchors/miniblue.crt
    sudo update-ca-trust
    ```

=== "Script"

    ```bash
    bash scripts/trust-cert.sh
    ```

    This auto-detects your OS and runs the appropriate command.

### Custom certificate directory

```bash
LOCAL_AZURE_CERT_DIR=/tmp/my-certs ./bin/miniblue
```

### Persistent storage

By default miniblue stores everything in memory (lost on restart). For persistence, set `DATABASE_URL`:

```bash
DATABASE_URL=postgres://user:pass@localhost:5432/miniblue ./bin/miniblue
```

Or use the included Postgres docker-compose:

```bash
docker-compose -f docker-compose.postgres.yml up
```

Persisted state (file or Postgres) is stamped with a schema version. If a miniblue release changes how state is stored, startup stops with a message naming both versions. Delete `~/.miniblue/state.json` (or `TRUNCATE miniblue_store` on Postgres) and restart. State is never reset automatically.

## Docker configuration

Pass environment variables with `-e`:

```bash
docker run -p 8080:8080 -p 8443:8443 \
  -e PORT=8080 \
  -e TLS_PORT=8443 \
  -e LOG_LEVEL=debug \
  moabukar/miniblue:latest
```

To access the generated certificate from the host, mount the cert directory:

```bash
docker run -p 4566:4566 -p 443:4567 -p 4567:4567 \
  -v ~/.miniblue:/root/.miniblue \
  moabukar/miniblue:latest
```

## Health check

```bash
curl http://localhost:4566/health
```

```json
{
  "services": [
    "subscriptions", "tenants", "resourcegroups", "blob", "table",
    "queue", "keyvault", "cosmosdb", "servicebus", "functions",
    "network", "dns", "acr", "eventgrid", "appconfig", "identity",
    "dbpostgres", "redis", "sqldb", "dbmysql", "publicip", "nsg",
    "loadbalancer", "appgw", "storageaccounts"
  ],
  "status": "running",
  "version": "0.2.5"
}
```

## Storage

miniblue uses **in-memory storage** by default. All data is lost when the process exits. This is intentional -- it keeps tests fast and repeatable.

!!! info "Persistent storage"
    File-backed persistent storage is on the [roadmap](https://github.com/moabukar/miniblue). Contributions welcome.
