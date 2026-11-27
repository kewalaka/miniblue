# Pulumi

miniblue works with Pulumi through dynamic providers. No Azure credentials are needed because the providers send HTTP calls to miniblue's REST API.

## Prerequisites

- miniblue running (`./bin/miniblue` or `docker run -p 4566:4566 moabukar/miniblue:latest`)
- Python 3.8+
- Pulumi CLI installed

## Quick start

```bash
# Install Pulumi
curl -fsSL https://get.pulumi.com | sh

# Use local state (no Pulumi Cloud account needed)
export PULUMI_CONFIG_PASSPHRASE=""
pulumi login --local

# Clone and run the example
cd examples/pulumi-python
python3 -m venv venv
source venv/bin/activate
pip install -r requirements.txt

# Deploy
pulumi stack init dev
pulumi up --yes --skip-preview
```

## How it works

The official `pulumi-azure-native` provider requires real Azure credentials and HTTPS endpoints. Instead, miniblue uses Pulumi dynamic providers, which are Python classes that send HTTP calls to `http://localhost:4566`.

Each provider implements `create()` and `delete()` methods that call the miniblue REST API.

## Example

The full working example is in `examples/pulumi-python/`. Here are the key providers:

### ResourceGroup

```python
import requests
import pulumi
from pulumi.dynamic import Resource, ResourceProvider, CreateResult

MINIBLUE_URL = "http://localhost:4566"
SUBSCRIPTION = "sub1"

class ResourceGroupProvider(ResourceProvider):
    def create(self, props):
        name = props["name"]
        location = props.get("location", "eastus")
        resp = requests.put(
            f"{MINIBLUE_URL}/subscriptions/{SUBSCRIPTION}/resourcegroups/{name}",
            json={"location": location, "tags": props.get("tags", {})},
        )
        resp.raise_for_status()
        return CreateResult(id_=name, outs=dict(props))

    def delete(self, id, props):
        requests.delete(
            f"{MINIBLUE_URL}/subscriptions/{SUBSCRIPTION}/resourcegroups/{id}"
        )

class ResourceGroup(Resource):
    def __init__(self, resource_name, name, location="eastus", tags=None, opts=None):
        super().__init__(ResourceGroupProvider(), resource_name, {
            "name": name, "location": location, "tags": tags or {},
        }, opts)
```

### KeyVault secret

```python
class KeyVaultSecretProvider(ResourceProvider):
    def create(self, props):
        vault = props["vault"]
        secret_name = props["secret_name"]
        resp = requests.put(
            f"{MINIBLUE_URL}/keyvault/{vault}/secrets/{secret_name}",
            json={"value": props["value"]},
        )
        resp.raise_for_status()
        return CreateResult(id_=f"{vault}/{secret_name}", outs=dict(props))

    def delete(self, id, props):
        requests.delete(
            f"{MINIBLUE_URL}/keyvault/{props['vault']}/secrets/{props['secret_name']}"
        )

class KeyVaultSecret(Resource):
    def __init__(self, resource_name, vault, secret_name, value, opts=None):
        super().__init__(KeyVaultSecretProvider(), resource_name, {
            "vault": vault, "secret_name": secret_name, "value": value,
        }, opts)
```

### Blob storage

```python
class BlobContainerProvider(ResourceProvider):
    def create(self, props):
        resp = requests.put(
            f"{MINIBLUE_URL}/blob/{props['account']}/{props['container']}"
        )
        resp.raise_for_status()
        return CreateResult(id_=f"{props['account']}/{props['container']}", outs=dict(props))

    def delete(self, id, props):
        requests.delete(
            f"{MINIBLUE_URL}/blob/{props['account']}/{props['container']}"
        )

class BlobProvider(ResourceProvider):
    def create(self, props):
        resp = requests.put(
            f"{MINIBLUE_URL}/blob/{props['account']}/{props['container']}/{props['blob_name']}",
            json=props.get("content", {}),
        )
        resp.raise_for_status()
        return CreateResult(
            id_=f"{props['account']}/{props['container']}/{props['blob_name']}",
            outs=dict(props),
        )

    def delete(self, id, props):
        requests.delete(
            f"{MINIBLUE_URL}/blob/{props['account']}/{props['container']}/{props['blob_name']}"
        )
```

### CosmosDB document

```python
class CosmosDBDocumentProvider(ResourceProvider):
    def create(self, props):
        resp = requests.post(
            f"{MINIBLUE_URL}/cosmosdb/{props['account']}/dbs/{props['db']}/colls/{props['collection']}/docs",
            json=props["document"],
        )
        resp.raise_for_status()
        doc_id = props["document"].get("id", "unknown")
        outs = dict(props)
        outs["doc_id"] = doc_id
        return CreateResult(
            id_=f"{props['account']}/{props['db']}/{props['collection']}/{doc_id}",
            outs=outs,
        )

    def delete(self, id, props):
        doc_id = props.get("doc_id", props["document"].get("id"))
        requests.delete(
            f"{MINIBLUE_URL}/cosmosdb/{props['account']}/dbs/{props['db']}/colls/{props['collection']}/docs/{doc_id}"
        )
```

### Stack definition

```python
rg = ResourceGroup("my-rg", name="pulumi-rg", location="eastus",
                    tags={"managed-by": "pulumi"})

secret = KeyVaultSecret("db-pass", vault="pulumi-vault",
                        secret_name="db-password", value="pulumi-secret-42",
                        opts=pulumi.ResourceOptions(depends_on=[rg]))

# Export outputs
pulumi.export("resource_group", "pulumi-rg")
pulumi.export("miniblue_url", MINIBLUE_URL)
```

## Supported resources

| Provider class | miniblue API | Operations |
|---------------|-------------|------------|
| `ResourceGroup` | ARM resource groups | Create, delete |
| `KeyVaultSecret` | Key Vault secrets | Create, delete |
| `BlobContainer` | Blob containers | Create, delete |
| `Blob` | Blob objects | Create, delete |
| `CosmosDBDocument` | Cosmos DB documents | Create, delete |

## Deploy and destroy

```bash
# Deploy all resources
pulumi up --yes --skip-preview

# Verify with azlocal
azlocal group list
azlocal keyvault secret list --vault pulumi-vault

# Tear down
pulumi destroy --yes --skip-preview
```

## Test with azlocal

After `pulumi up`, verify that Pulumi-created resources exist in miniblue:

```bash
# Resource group
azlocal group show --name pulumi-rg

# Key Vault secret
azlocal keyvault secret show --vault pulumi-vault --name db-password

# Blob
azlocal storage blob list --account pulumiaccount --container appdata

# CosmosDB document
azlocal cosmosdb doc show --account pulumiaccount --database appdb \
  --collection users --id user1
```

Or verify with curl:

```bash
# Resource group
curl -s http://localhost:4566/subscriptions/sub1/resourcegroups/pulumi-rg | jq .

# Secret
curl -s http://localhost:4566/keyvault/pulumi-vault/secrets/db-password | jq .value

# Blob
curl -s http://localhost:4566/blob/pulumiaccount/appdata/config.json | jq .
```

## Pulumi.yaml

```yaml
name: miniblue-pulumi-python
runtime: python
description: Pulumi Python example for miniblue (local Azure emulator)
```

## requirements.txt

```
pulumi>=3.0.0
requests
```
