# azlocal CLI

`azlocal` is a command-line client for miniblue, similar to `awslocal` for LocalStack. It wraps HTTP calls to the miniblue API -- no authentication required.

## Installation

```bash
# Build from source
git clone https://github.com/moabukar/miniblue.git
cd miniblue
make build

# The binary is at bin/azlocal

# Install globally (optional)
sudo make install
# -> copies to /usr/local/bin/azlocal
```

Or with `go install`:

```bash
go install github.com/moabukar/miniblue/cmd/azlocal@latest
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `LOCAL_AZURE_ENDPOINT` | `http://localhost:4566` | miniblue endpoint URL |

```bash
# Point to a remote miniblue instance
export LOCAL_AZURE_ENDPOINT=http://192.168.1.100:4566
azlocal health
```

## Commands

### health

Check if miniblue is running:

```bash
azlocal health
```

```json
{
  "services": ["subscriptions","tenants","resourcegroups","blob","table","queue","keyvault","cosmosdb","servicebus","functions","network","dns","acr","eventgrid","appconfig","identity"],
  "status": "running",
  "version": "0.2.5"
}
```

---

### group (Resource Groups)

```bash
# Create a resource group
azlocal group create --name myRG --location eastus

# Create with a specific subscription
azlocal group create --name myRG --location westus2 --subscription my-sub-id

# List all resource groups
azlocal group list
azlocal group list --subscription my-sub-id

# Show a specific resource group
azlocal group show --name myRG
azlocal group show --name myRG --subscription my-sub-id

# Delete a resource group
azlocal group delete --name myRG
azlocal group delete --name myRG --subscription my-sub-id
```

!!! note
    If `--subscription` is omitted, it defaults to `00000000-0000-0000-0000-000000000000`.

---

### keyvault (Key Vault)

```bash
# Set a secret
azlocal keyvault secret set --vault myvault --name db-password --value "P@ssw0rd123!"

# Get a secret
azlocal keyvault secret show --vault myvault --name db-password

# List all secrets in a vault
azlocal keyvault secret list --vault myvault

# Delete a secret
azlocal keyvault secret delete --vault myvault --name db-password
```

---

### storage (Blob Storage)

#### Containers

```bash
# Create a container
azlocal storage container create --account myaccount --name mycontainer

# Delete a container
azlocal storage container delete --account myaccount --name mycontainer
```

#### Blobs

```bash
# Upload from a string
azlocal storage blob upload --account myaccount --container mycontainer \
  --name hello.txt --data "Hello from miniblue!"

# Upload from a file
azlocal storage blob upload --account myaccount --container mycontainer \
  --name config.json --file ./config.json

# Download a blob
azlocal storage blob download --account myaccount --container mycontainer \
  --name hello.txt

# List blobs in a container
azlocal storage blob list --account myaccount --container mycontainer

# Delete a blob
azlocal storage blob delete --account myaccount --container mycontainer \
  --name hello.txt
```

---

### network (Virtual Networks)

```bash
# Create a VNet
azlocal network vnet create --name my-vnet --resource-group myRG \
  --address-prefix 10.0.0.0/16 --location eastus

# Show a VNet
azlocal network vnet show --name my-vnet --resource-group myRG

# List VNets
azlocal network vnet list --resource-group myRG

# Delete a VNet
azlocal network vnet delete --name my-vnet --resource-group myRG

# Create a subnet inside a VNet
azlocal network vnet subnet create --resource-group myRG --vnet-name my-vnet \
  --name my-subnet --address-prefixes 10.0.1.0/24

# Show a subnet
azlocal network vnet subnet show --resource-group myRG --vnet-name my-vnet --name my-subnet

# List subnets in a VNet
azlocal network vnet subnet list --resource-group myRG --vnet-name my-vnet

# Update a subnet (re-issues a PUT with the new properties)
azlocal network vnet subnet update --resource-group myRG --vnet-name my-vnet \
  --name my-subnet --address-prefixes 10.0.2.0/24

# Delete a subnet
azlocal network vnet subnet delete --resource-group myRG --vnet-name my-vnet --name my-subnet
```

All network commands require `--resource-group`. Optionally pass `--subscription` (defaults to `00000000-0000-0000-0000-000000000000`).

---

### vm (Virtual Machines)

```bash
# Create a VM
azlocal vm create --resource-group myRG --name my-vm \
  --image Ubuntu2204 --size Standard_B1s --admin-username azureuser

# Show a VM
azlocal vm show --resource-group myRG --name my-vm

# List VMs
azlocal vm list --resource-group myRG

# Stop and inspect a VM
azlocal vm stop --resource-group myRG --name my-vm
azlocal vm get-instance-view --resource-group myRG --name my-vm

# Delete a VM
azlocal vm delete --resource-group myRG --name my-vm

# Manage VM extensions
azlocal vm extension set --resource-group myRG --vm-name my-vm \
  --name customScript --publisher Microsoft.Azure.Extensions \
  --type CustomScript --settings '{"commandToExecute":"echo ok"}'
azlocal vm extension list --resource-group myRG --vm-name my-vm
azlocal vm extension delete --resource-group myRG --vm-name my-vm --name customScript
```

VM commands support `create`, `show`, `list`, `update`, `delete`, `start`, `stop`, `restart`, `deallocate`, `redeploy`, and `get-instance-view`.

---

### cosmosdb (Cosmos DB)

```bash
# Create a document
azlocal cosmosdb doc create --account myaccount --database mydb \
  --collection mycoll --id doc1 --data '{"name":"Alice","age":30}'

# Get a document
azlocal cosmosdb doc show --account myaccount --database mydb \
  --collection mycoll --id doc1

# List all documents
azlocal cosmosdb doc list --account myaccount --database mydb \
  --collection mycoll

# Delete a document
azlocal cosmosdb doc delete --account myaccount --database mydb \
  --collection mycoll --id doc1
```

---

### servicebus (Service Bus)

#### Queues

```bash
# Create a queue
azlocal servicebus queue create --namespace my-ns --name my-queue

# Send a message
azlocal servicebus queue send --namespace my-ns --name my-queue \
  --body "Hello from Service Bus!"

# Receive (peek) a message
azlocal servicebus queue receive --namespace my-ns --name my-queue

# Delete a queue
azlocal servicebus queue delete --namespace my-ns --name my-queue
```

#### Topics

```bash
# Create a topic
azlocal servicebus topic create --namespace my-ns --name my-topic

# Publish a message
azlocal servicebus topic send --namespace my-ns --name my-topic \
  --body "Broadcast message"

# Delete a topic
azlocal servicebus topic delete --namespace my-ns --name my-topic
```

---

### appconfig (App Configuration)

```bash
# Set a key-value pair
azlocal appconfig kv set --store mystore --key feature-flag --value "true"

# Get a value
azlocal appconfig kv show --store mystore --key feature-flag

# List all key-value pairs
azlocal appconfig kv list --store mystore

# Delete a key-value pair
azlocal appconfig kv delete --store mystore --key feature-flag
```

---

### functionapp (Azure Functions)

```bash
# Create a function app
azlocal functionapp create --name my-func --resource-group myRG --location eastus

# Show a function app
azlocal functionapp show --name my-func --resource-group myRG

# List function apps
azlocal functionapp list --resource-group myRG

# Delete a function app
azlocal functionapp delete --name my-func --resource-group myRG
```

---

### identity (User-Assigned Managed Identities)

```bash
azlocal identity create --resource-group myRG --name myid --location eastus
azlocal identity list   --resource-group myRG
azlocal identity show   --resource-group myRG --name myid
azlocal identity update --resource-group myRG --name myid --tags "env=prod"
azlocal identity delete --resource-group myRG --name myid
```

---

### role (Azure RBAC)

Role assignments and definitions are stored but **never enforced** — every
other miniblue service still serves every request without authorization
checks. The commands exist so Terraform's `azurerm_role_assignment` and
`az role` workflows plan and apply cleanly against miniblue.

```bash
# Role assignments
azlocal role assignment create --assignee 11111111-1111-1111-1111-111111111111 \
  --role Contributor --resource-group myRG
azlocal role assignment list   --resource-group myRG
azlocal role assignment delete --name <assignment-guid> --resource-group myRG

# Role definitions (a fixed catalog of built-in roles is always available)
azlocal role definition list
azlocal role definition show --name Reader
azlocal role definition create --name MyCustomRole \
  --description "Read storage accounts"
```

When `--name` is omitted on `role assignment create`, azlocal derives a
deterministic GUID from `{scope, principalId, roleDefinitionId}` so repeated
applies remain idempotent.

---

### storage account management-policy (Lifecycle policies)

```bash
azlocal storage account management-policy create --name myacct \
  --resource-group myRG --policy @policy.json
azlocal storage account management-policy show   --name myacct --resource-group myRG
azlocal storage account management-policy delete --name myacct --resource-group myRG
```

`--policy` accepts either an inline JSON string, a file path, or `@file`.
miniblue round-trips the rule bodies but does not enforce them against
blob data.

---

## Full command reference

| Command | Subcommands |
|---------|-------------|
| `azlocal health` | _(none)_ |
| `azlocal group` | `create`, `list`, `show`, `delete` |
| `azlocal keyvault secret` | `set`, `show`, `list`, `delete` |
| `azlocal storage account` | `create`, `list`, `show`, `delete`, `list-keys`, `management-policy` |
| `azlocal storage account management-policy` | `create`, `update`, `show`, `delete` |
| `azlocal storage container` | `create`, `delete` |
| `azlocal storage blob` | `upload`, `download`, `list`, `delete` |
| `azlocal network vnet` | `create`, `show`, `list`, `delete` |
| `azlocal network vnet subnet` | `create`, `show`, `list`, `delete`, `update` |
| `azlocal vm` | `create`, `show`, `list`, `update`, `delete`, `start`, `stop`, `restart`, `deallocate`, `redeploy`, `get-instance-view` |
| `azlocal vm extension` | `set`, `show`, `list`, `update`, `delete` |
| `azlocal identity` | `create`, `list`, `show`, `update`, `delete` |
| `azlocal role assignment` | `create`, `list`, `show`, `delete` |
| `azlocal role definition` | `create`, `list`, `show`, `delete` |
| `azlocal cosmosdb doc` | `create`, `show`, `list`, `delete` |
| `azlocal servicebus queue` | `create`, `send`, `receive`, `delete` |
| `azlocal servicebus topic` | `create`, `send`, `delete` |
| `azlocal appconfig kv` | `set`, `show`, `list`, `delete` |
| `azlocal functionapp` | `create`, `show`, `list`, `delete` |

## Scripting example

```bash
#!/bin/bash
set -e

# Set up infrastructure
azlocal group create --name dev-rg --location eastus
azlocal keyvault secret set --vault dev-vault --name api-key --value "sk-12345"
azlocal storage container create --account devstore --name uploads
azlocal storage blob upload --account devstore --container uploads \
  --name seed-data.json --file ./fixtures/seed-data.json

echo "Local environment ready."
```
