# Azure Cache for Redis

miniblue emulates Azure Cache for Redis via ARM endpoints. When `REDIS_URL` is set, miniblue verifies connectivity to a real Redis instance. Without it, the API works as a metadata-only stub.

## Environment variable

| Variable | Example | Effect |
|----------|---------|--------|
| `REDIS_URL` | `redis://localhost:6379` | Enables connectivity check on cache creation |

```bash
# With real Redis backend
REDIS_URL=redis://localhost:6379 ./bin/miniblue

# Or stub-only (no real Redis needed)
./bin/miniblue
```

## API endpoints

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Cache/redis/{name}` | Create or update cache |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Cache/redis/{name}` | Get cache |
| `DELETE` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Cache/redis/{name}` | Delete cache |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Cache/redis` | List caches |
| `POST` | `.../redis/{name}/listKeys` | Get access keys |

## Create a cache

```bash
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Cache/redis/my-cache" \
  -H "Content-Type: application/json" \
  -d '{
    "location": "eastus",
    "properties": {
      "sku": {
        "name": "Basic",
        "family": "C",
        "capacity": 1
      }
    }
  }'
```

Response (`201 Created`):

```json
{
  "id": "/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Cache/redis/my-cache",
  "name": "my-cache",
  "type": "Microsoft.Cache/redis",
  "location": "eastus",
  "properties": {
    "provisioningState": "Succeeded",
    "hostName": "localhost",
    "port": 6379,
    "sslPort": 6380,
    "redisVersion": "7.2",
    "sku": {
      "name": "Basic",
      "family": "C",
      "capacity": 1
    },
    "enableNonSslPort": true,
    "publicNetworkAccess": "Enabled"
  }
}
```

If `REDIS_URL` is set and the Redis instance is unreachable, the API returns `503 Service Unavailable`.

## Get a cache

```bash
curl "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Cache/redis/my-cache"
```

## List caches

```bash
curl "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Cache/redis"
```

## Delete a cache

```bash
curl -X DELETE "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Cache/redis/my-cache"
```

The endpoint returns `200 OK`.

## List keys

```bash
curl -X POST "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Cache/redis/my-cache/listKeys"
```

Response:

```json
{
  "primaryKey": "miniblue-redis-key",
  "secondaryKey": "miniblue-redis-key-2"
}
```

The keys are static mock values. The cache must exist or this returns `404`.

## azlocal

```bash
# Use curl for Redis ARM endpoints (azlocal does not yet have redis commands)

# Create cache
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Cache/redis/my-cache" \
  -H "Content-Type: application/json" -d '{"location":"eastus"}'

# List keys
curl -X POST "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Cache/redis/my-cache/listKeys"

# Verify the resource group
azlocal group show --name myRG
```

## Full example

```bash
#!/bin/bash
set -e

BASE="http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Cache/redis"

# Create resource group
curl -s -X PUT "http://localhost:4566/subscriptions/sub1/resourcegroups/myRG" \
  -H "Content-Type: application/json" \
  -d '{"location": "eastus"}'

# Create cache
curl -s -X PUT "${BASE}/my-cache" \
  -H "Content-Type: application/json" \
  -d '{"location": "eastus"}' | jq '{name: .name, host: .properties.hostName, port: .properties.port}'

# Get access keys
curl -s -X POST "${BASE}/my-cache/listKeys" | jq .

# Get cache details
curl -s "${BASE}/my-cache" | jq .properties.provisioningState

# List all caches
curl -s "${BASE}" | jq '.value[].name'

# Clean up
curl -s -X DELETE "${BASE}/my-cache"
```

## Limitations

- The ARM API does not perform Redis data operations. Use a real Redis client for data.
- Access keys are static mock values (`miniblue-redis-key`, `miniblue-redis-key-2`).
- Firewall rules, private endpoints, and clustering configuration are not supported.
- `REDIS_URL` is only used for a TCP connectivity check on cache creation; miniblue does not proxy Redis commands.
