# Database for PostgreSQL

miniblue emulates Azure Database for PostgreSQL Flexible Servers via ARM endpoints. When `POSTGRES_URL` is set, miniblue creates databases on a real PostgreSQL instance. Without it, the API works as a metadata-only stub.

## Environment variable

| Variable | Example | Effect |
|----------|---------|--------|
| `POSTGRES_URL` | `postgres://user:pass@localhost:5432/postgres` | Enables real database creation/deletion |

```bash
# With real Postgres backend
POSTGRES_URL=postgres://user:pass@localhost:5432/postgres ./bin/miniblue

# Or stub-only (no real Postgres needed)
./bin/miniblue
```

## API endpoints

### Flexible servers

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.DBforPostgreSQL/flexibleServers/{name}` | Create or update server |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.DBforPostgreSQL/flexibleServers/{name}` | Get server |
| `DELETE` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.DBforPostgreSQL/flexibleServers/{name}` | Delete server |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.DBforPostgreSQL/flexibleServers` | List servers |

### Databases

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `.../flexibleServers/{server}/databases/{db}` | Create or update database |
| `GET` | `.../flexibleServers/{server}/databases/{db}` | Get database |
| `DELETE` | `.../flexibleServers/{server}/databases/{db}` | Delete database |
| `GET` | `.../flexibleServers/{server}/databases` | List databases |

## Create a flexible server

```bash
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.DBforPostgreSQL/flexibleServers/my-pg-server" \
  -H "Content-Type: application/json" \
  -d '{
    "location": "eastus",
    "properties": {
      "administratorLogin": "pgadmin",
      "version": "16"
    },
    "sku": {
      "name": "Standard_B1ms",
      "tier": "Burstable"
    }
  }'
```

Response (`201 Created`):

```json
{
  "id": "/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.DBforPostgreSQL/flexibleServers/my-pg-server",
  "name": "my-pg-server",
  "type": "Microsoft.DBforPostgreSQL/flexibleServers",
  "location": "eastus",
  "properties": {
    "fullyQualifiedDomainName": "localhost",
    "state": "Ready",
    "version": "16",
    "administratorLogin": "miniblue",
    "storage": { "storageSizeGB": 32 },
    "backup": { "backupRetentionDays": 7 },
    "network": { "publicNetworkAccess": "Enabled" },
    "highAvailability": { "mode": "Disabled" }
  },
  "sku": { "name": "Standard_B1ms", "tier": "Burstable" }
}
```

## Get a server

```bash
curl "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.DBforPostgreSQL/flexibleServers/my-pg-server"
```

## List servers

```bash
curl "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.DBforPostgreSQL/flexibleServers"
```

## Delete a server

```bash
curl -X DELETE "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.DBforPostgreSQL/flexibleServers/my-pg-server"
```

This request returns `202 Accepted`. If `POSTGRES_URL` is set, miniblue drops all real databases under this server.

## Create a database

The parent server must exist first.

```bash
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.DBforPostgreSQL/flexibleServers/my-pg-server/databases/appdb" \
  -H "Content-Type: application/json" \
  -d '{}'
```

Response (`201 Created`):

```json
{
  "id": "/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.DBforPostgreSQL/flexibleServers/my-pg-server/databases/appdb",
  "name": "appdb",
  "type": "Microsoft.DBforPostgreSQL/flexibleServers/databases",
  "properties": {
    "charset": "UTF8",
    "collation": "en_US.utf8"
  }
}
```

If `POSTGRES_URL` is set, miniblue runs `CREATE DATABASE appdb` on the PostgreSQL instance.

## Get a database

```bash
curl "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.DBforPostgreSQL/flexibleServers/my-pg-server/databases/appdb"
```

## List databases

```bash
curl "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.DBforPostgreSQL/flexibleServers/my-pg-server/databases"
```

## Delete a database

```bash
curl -X DELETE "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.DBforPostgreSQL/flexibleServers/my-pg-server/databases/appdb"
```

This request returns `202 Accepted`. If `POSTGRES_URL` is set, miniblue runs `DROP DATABASE appdb` on the PostgreSQL instance.

## azlocal

```bash
# Servers -- use curl (azlocal does not yet have postgres commands)
# Create server
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.DBforPostgreSQL/flexibleServers/my-pg-server" \
  -H "Content-Type: application/json" -d '{"location":"eastus"}'

# Create database
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.DBforPostgreSQL/flexibleServers/my-pg-server/databases/mydb" \
  -H "Content-Type: application/json" -d '{}'

# Verify with azlocal group (the resource group)
azlocal group show --name myRG
```

## Full example

```bash
#!/bin/bash
set -e

BASE="http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.DBforPostgreSQL/flexibleServers"

# Create resource group first
curl -s -X PUT "http://localhost:4566/subscriptions/sub1/resourcegroups/myRG" \
  -H "Content-Type: application/json" \
  -d '{"location": "eastus"}'

# Create server
curl -s -X PUT "${BASE}/my-pg" \
  -H "Content-Type: application/json" \
  -d '{"location": "eastus"}' | jq .name

# Create two databases
curl -s -X PUT "${BASE}/my-pg/databases/users_db" \
  -H "Content-Type: application/json" -d '{}' | jq .name
curl -s -X PUT "${BASE}/my-pg/databases/orders_db" \
  -H "Content-Type: application/json" -d '{}' | jq .name

# List databases
curl -s "${BASE}/my-pg/databases" | jq '.value[].name'

# Clean up
curl -s -X DELETE "${BASE}/my-pg"
```

## Limitations

- miniblue accepts request body properties such as SKU, storage size, and version, but ignores them and uses fixed defaults.
- No firewall rules or private endpoints.
- Real PostgreSQL operations require `POSTGRES_URL` to point to an accessible PostgreSQL instance.
