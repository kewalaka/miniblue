# Azure SQL Database

miniblue emulates Azure SQL Database via ARM endpoints. This stub implementation stores server and database metadata, but it does not connect to a real SQL Server instance.

## API endpoints

### SQL servers

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Sql/servers/{name}` | Create or update server |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Sql/servers/{name}` | Get server |
| `DELETE` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Sql/servers/{name}` | Delete server |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Sql/servers` | List servers |

### Databases

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `.../servers/{server}/databases/{db}` | Create or update database |
| `GET` | `.../servers/{server}/databases/{db}` | Get database |
| `DELETE` | `.../servers/{server}/databases/{db}` | Delete database |
| `GET` | `.../servers/{server}/databases` | List databases |

## Create a SQL server

```bash
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Sql/servers/my-sql-server" \
  -H "Content-Type: application/json" \
  -d '{
    "location": "eastus",
    "properties": {
      "administratorLogin": "sqladmin"
    }
  }'
```

Response (`201 Created`):

```json
{
  "id": "/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Sql/servers/my-sql-server",
  "name": "my-sql-server",
  "type": "Microsoft.Sql/servers",
  "location": "eastus",
  "properties": {
    "fullyQualifiedDomainName": "localhost",
    "state": "Ready",
    "version": "12.0",
    "administratorLogin": "sqladmin",
    "provisioningState": "Succeeded"
  }
}
```

## Get a server

```bash
curl "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Sql/servers/my-sql-server"
```

## List servers

```bash
curl "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Sql/servers"
```

## Delete a server

```bash
curl -X DELETE "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Sql/servers/my-sql-server"
```

This request returns `202 Accepted`. Deleting a server also removes all child database metadata.

## Create a database

The parent server must exist first.

```bash
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Sql/servers/my-sql-server/databases/appdb" \
  -H "Content-Type: application/json" \
  -d '{
    "location": "eastus",
    "sku": {
      "name": "Basic",
      "tier": "Basic"
    }
  }'
```

Response (`201 Created`):

```json
{
  "id": "/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Sql/servers/my-sql-server/databases/appdb",
  "name": "appdb",
  "type": "Microsoft.Sql/servers/databases",
  "location": "eastus",
  "properties": {
    "status": "Online",
    "collation": "SQL_Latin1_General_CP1_CI_AS",
    "maxSizeBytes": 2147483648,
    "currentServiceObjectiveName": "Basic",
    "provisioningState": "Succeeded"
  },
  "sku": {
    "name": "Basic",
    "tier": "Basic",
    "capacity": 5
  }
}
```

## Get a database

```bash
curl "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Sql/servers/my-sql-server/databases/appdb"
```

## List databases

```bash
curl "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Sql/servers/my-sql-server/databases"
```

## Delete a database

```bash
curl -X DELETE "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Sql/servers/my-sql-server/databases/appdb"
```

This request returns `202 Accepted`.

## azlocal

```bash
# Use curl for SQL ARM endpoints (azlocal does not yet have sql commands)

# Create server
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Sql/servers/my-sql-server" \
  -H "Content-Type: application/json" -d '{"location":"eastus"}'

# Create database
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Sql/servers/my-sql-server/databases/mydb" \
  -H "Content-Type: application/json" -d '{"location":"eastus"}'

# Verify the resource group
azlocal group show --name myRG
```

## Full example

```bash
#!/bin/bash
set -e

BASE="http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Sql/servers"

# Create resource group
curl -s -X PUT "http://localhost:4566/subscriptions/sub1/resourcegroups/myRG" \
  -H "Content-Type: application/json" \
  -d '{"location": "eastus"}'

# Create SQL server
curl -s -X PUT "${BASE}/my-sql" \
  -H "Content-Type: application/json" \
  -d '{"location": "eastus", "properties": {"administratorLogin": "sqladmin"}}' | jq .name

# Create databases
curl -s -X PUT "${BASE}/my-sql/databases/production" \
  -H "Content-Type: application/json" -d '{"location":"eastus"}' | jq .name
curl -s -X PUT "${BASE}/my-sql/databases/staging" \
  -H "Content-Type: application/json" -d '{"location":"eastus"}' | jq .name

# List databases
curl -s "${BASE}/my-sql/databases" | jq '.value[].name'

# Clean up
curl -s -X DELETE "${BASE}/my-sql"
```

## Limitations

- Stub implementation with no SQL Server backend. miniblue stores server and database metadata in memory.
- miniblue accepts and echoes request body properties such as SKU, location, and collation, but they have no backend effect.
- No elastic pools, firewall rules, or auditing.
