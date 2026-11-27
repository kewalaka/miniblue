# Container Instances

miniblue emulates Azure Container Instances (ACI) via ARM endpoints. When Docker is available on the host, miniblue creates real Docker containers. Without Docker, it works as a metadata-only stub.

## Docker detection

miniblue checks for Docker availability at startup by looking for `/var/run/docker.sock` and running `docker info`. The log shows:

```
[aci] Docker is available -- container groups will use real containers
```

or:

```
[aci] Docker is NOT available -- container groups will be stub-only
```

## API endpoints

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.ContainerInstance/containerGroups/{name}` | Create or update container group |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.ContainerInstance/containerGroups/{name}` | Get container group |
| `DELETE` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.ContainerInstance/containerGroups/{name}` | Delete container group |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.ContainerInstance/containerGroups` | List container groups |

## Create a container group

```bash
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.ContainerInstance/containerGroups/my-app" \
  -H "Content-Type: application/json" \
  -d '{
    "location": "eastus",
    "properties": {
      "osType": "Linux",
      "containers": [
        {
          "name": "web",
          "properties": {
            "image": "nginx:latest",
            "ports": [
              { "port": 8080 }
            ],
            "environmentVariables": [
              { "name": "APP_ENV", "value": "development" }
            ],
            "resources": {
              "requests": { "cpu": 1, "memoryInGB": 1.5 }
            }
          }
        }
      ],
      "ipAddress": {
        "type": "Public",
        "ports": [
          { "protocol": "TCP", "port": 8080 }
        ]
      }
    }
  }'
```

Response (`201 Created`):

```json
{
  "id": "/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.ContainerInstance/containerGroups/my-app",
  "name": "my-app",
  "type": "Microsoft.ContainerInstance/containerGroups",
  "location": "eastus",
  "properties": {
    "provisioningState": "Succeeded",
    "containers": [
      {
        "name": "web",
        "properties": {
          "image": "nginx:latest",
          "ports": [{ "port": 8080 }],
          "instanceView": {
            "currentState": {
              "state": "Running",
              "startTime": "2026-01-01T00:00:00Z"
            }
          }
        }
      }
    ],
    "osType": "Linux",
    "instanceView": {
      "state": "Running"
    },
    "ipAddress": {
      "ip": "127.0.0.1",
      "type": "Public",
      "ports": [{ "protocol": "TCP", "port": 8080 }]
    }
  }
}
```

When Docker is available, miniblue runs `docker run -d --name miniblue-my-app -e APP_ENV=development -p 8080:8080 nginx:latest` and prefixes the container name with `miniblue-`.

## Get a container group

```bash
curl "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.ContainerInstance/containerGroups/my-app"
```

When Docker is available, miniblue refreshes the `state` field from the real container using `docker inspect`.

## List container groups

```bash
curl "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.ContainerInstance/containerGroups"
```

## Delete a container group

```bash
curl -X DELETE "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.ContainerInstance/containerGroups/my-app"
```

This request returns `204 No Content`. When Docker is available, miniblue runs `docker stop` and `docker rm` on the container.

## Container spec reference

The request body follows the Azure ACI format. miniblue reads these fields from `properties.containers[0].properties`:

| Field | Type | Effect with Docker |
|-------|------|--------------------|
| `image` | string | Docker image to pull and run |
| `ports` | array of `{ port: number }` | Mapped as `-p port:port` |
| `environmentVariables` | array of `{ name, value }` | Passed as `-e name=value` |
| `command` | array of strings | Appended as container command |

## azlocal

```bash
# Use curl for ACI ARM endpoints (azlocal does not yet have aci commands)

# Create container group
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.ContainerInstance/containerGroups/my-app" \
  -H "Content-Type: application/json" \
  -d '{
    "location": "eastus",
    "properties": {
      "osType": "Linux",
      "containers": [{
        "name": "web",
        "properties": {
          "image": "nginx:latest",
          "ports": [{"port": 80}]
        }
      }]
    }
  }'

# Check status
curl -s "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.ContainerInstance/containerGroups/my-app" \
  | jq .properties.instanceView.state

# Verify the resource group
azlocal group show --name myRG
```

## Full example

```bash
#!/bin/bash
set -e

BASE="http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.ContainerInstance/containerGroups"

# Create resource group
curl -s -X PUT "http://localhost:4566/subscriptions/sub1/resourcegroups/myRG" \
  -H "Content-Type: application/json" \
  -d '{"location": "eastus"}'

# Run an nginx container
curl -s -X PUT "${BASE}/web-app" \
  -H "Content-Type: application/json" \
  -d '{
    "location": "eastus",
    "properties": {
      "osType": "Linux",
      "containers": [{
        "name": "nginx",
        "properties": {
          "image": "nginx:alpine",
          "ports": [{"port": 8080}],
          "environmentVariables": [
            {"name": "NGINX_PORT", "value": "8080"}
          ]
        }
      }],
      "ipAddress": {
        "type": "Public",
        "ports": [{"protocol": "TCP", "port": 8080}]
      }
    }
  }' | jq '{name: .name, state: .properties.instanceView.state}'

# List container groups
curl -s "${BASE}" | jq '.value[].name'

# Check state (with Docker, this reflects the real container status)
curl -s "${BASE}/web-app" | jq .properties.instanceView.state

# Clean up (stops and removes the real Docker container too)
curl -s -X DELETE "${BASE}/web-app"
```

## Limitations

- miniblue runs only the first container in the group in Docker
- No volume mounts, init containers, or GPU resources
- The IP address is always `127.0.0.1`
- Without Docker, all operations are metadata-only stubs
- miniblue prefixes Docker container names with `miniblue-`, for example `miniblue-my-app`
