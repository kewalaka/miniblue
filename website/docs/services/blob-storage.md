# Blob Storage

miniblue emulates Azure Blob Storage with container and blob CRUD operations. Data is stored in memory.

## API endpoints

### Containers

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/blob/{account}/{container}` | Create container |
| `GET` | `/blob/{account}/{container}` | List blobs in container |
| `DELETE` | `/blob/{account}/{container}` | Delete container |

### Blobs

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/blob/{account}/{container}/{blob}` | Upload blob (block blob) |
| `GET` | `/blob/{account}/{container}/{blob}` | Download blob |
| `HEAD` | `/blob/{account}/{container}/{blob}` | Get blob properties without body |
| `DELETE` | `/blob/{account}/{container}/{blob}` | Delete blob |
| `PUT` | `/blob/{account}/{container}/{blob}?comp=metadata` | Set blob metadata (`x-ms-meta-*` headers) |
| `GET` | `/blob/{account}/{container}/{blob}?comp=metadata` | Get blob metadata |
| `PUT` | `/blob/{account}/{container}/{blob}?comp=properties` | Set blob system properties (`x-ms-blob-*` headers) |
| `PUT` | `/blob/{account}/{container}/{blob}?comp=lease` | Lease operations (acquire/renew/change/release/break) |

Blob names may contain forward slashes (e.g. `env:/prod/terraform.tfstate`) —
miniblue treats everything after the container segment as the blob name so
multi-segment paths used by the Terraform `azurerm` backend work as expected.

### Authentication

The blob data plane verifies Azure SharedKey signatures using the
auto-generated account keys returned by `listKeys`. For local scripting
where signing is impractical, set `MINIBLUE_DISABLE_SHAREDKEY_AUTH=1`
(accepted values: `1`, `true`, `yes`, `on`) to bypass signature verification
entirely. This is **dev-only** — never enable it in an environment that
pretends to model production.

## Create a container

```bash
curl -X PUT "http://localhost:4566/blob/myaccount/mycontainer"
```

Response: `201 Created`

## Upload a blob

```bash
curl -X PUT "http://localhost:4566/blob/myaccount/mycontainer/hello.txt" \
  -H "Content-Type: text/plain" \
  -d "Hello from miniblue!"
```

Response: `201 Created`

Upload a JSON file:

```bash
curl -X PUT "http://localhost:4566/blob/myaccount/mycontainer/config.json" \
  -H "Content-Type: application/json" \
  -d '{"database": "localhost", "port": 5432}'
```

Upload a binary file:

```bash
curl -X PUT "http://localhost:4566/blob/myaccount/mycontainer/image.png" \
  -H "Content-Type: image/png" \
  --data-binary @./image.png
```

## Download a blob

```bash
curl "http://localhost:4566/blob/myaccount/mycontainer/hello.txt"
```

```
Hello from miniblue!
```

The response includes Azure-compatible headers:

| Header | Example |
|--------|---------|
| `Content-Type` | `text/plain` |
| `Content-Length` | `20` |
| `ETag` | `"0x1A2B3C4D5E6F"` |

## List blobs in a container

```bash
curl "http://localhost:4566/blob/myaccount/mycontainer"
```

`List Blobs` returns an Azure spec-compliant XML `EnumerationResults`
document by default (matching what the Azure SDKs and the Terraform
`azurerm` backend expect). The legacy JSON shape is preserved for tooling
that requests `Accept: application/json`:

```json
{
  "blobs": [
    {
      "name": "hello.txt",
      "properties": {
        "contentLength": "20",
        "contentType": "text/plain",
        "etag": "\"0x1A2B3C4D5E6F\"",
        "lastModified": "Mon, 01 Jan 2026 00:00:00 UTC"
      }
    }
  ]
}
```

## Lease a blob

miniblue implements the `Lease Blob` API (acquire, renew, change, release,
break) needed by the Terraform `azurerm` backend to coordinate concurrent
state writes.

```bash
# Acquire a 60-second lease
curl -X PUT "http://localhost:4566/blob/myaccount/mycontainer/terraform.tfstate?comp=lease" \
  -H "x-ms-lease-action: acquire" \
  -H "x-ms-lease-duration: 60"

# Renew
curl -X PUT "http://localhost:4566/blob/myaccount/mycontainer/terraform.tfstate?comp=lease" \
  -H "x-ms-lease-action: renew" \
  -H "x-ms-lease-id: <lease-id>"

# Release
curl -X PUT "http://localhost:4566/blob/myaccount/mycontainer/terraform.tfstate?comp=lease" \
  -H "x-ms-lease-action: release" \
  -H "x-ms-lease-id: <lease-id>"
```

Lease state is surfaced as `x-ms-lease-status`, `x-ms-lease-state` and
`x-ms-lease-duration` headers on `GET`/`HEAD`/`List Blobs`.

## Delete a blob

```bash
curl -X DELETE "http://localhost:4566/blob/myaccount/mycontainer/hello.txt"
```

Response: `202 Accepted`

## Delete a container

```bash
curl -X DELETE "http://localhost:4566/blob/myaccount/mycontainer"
```

Response: `202 Accepted`

## azlocal

```bash
# Containers
azlocal storage container create --account myaccount --name mycontainer
azlocal storage container delete --account myaccount --name mycontainer

# Blobs
azlocal storage blob upload --account myaccount --container mycontainer \
  --name hello.txt --data "Hello from miniblue!"

azlocal storage blob upload --account myaccount --container mycontainer \
  --name config.json --file ./config.json

azlocal storage blob download --account myaccount --container mycontainer \
  --name hello.txt

azlocal storage blob list --account myaccount --container mycontainer

azlocal storage blob delete --account myaccount --container mycontainer \
  --name hello.txt
```

## Full example

```bash
#!/bin/bash
set -e

ACCOUNT="devaccount"
CONTAINER="uploads"

# Create container
curl -X PUT "http://localhost:4566/blob/${ACCOUNT}/${CONTAINER}"

# Upload three files
for f in file1.txt file2.txt file3.txt; do
  curl -X PUT "http://localhost:4566/blob/${ACCOUNT}/${CONTAINER}/${f}" \
    -H "Content-Type: text/plain" \
    -d "Content of ${f}"
done

# List all blobs
curl -s "http://localhost:4566/blob/${ACCOUNT}/${CONTAINER}" | jq '.blobs[].name'

# Download one
curl "http://localhost:4566/blob/${ACCOUNT}/${CONTAINER}/file1.txt"

# Clean up
curl -X DELETE "http://localhost:4566/blob/${ACCOUNT}/${CONTAINER}"
```
