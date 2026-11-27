# SDK usage

miniblue exposes a plain HTTP REST API. The official Azure SDKs enforce HTTPS and real credentials, so use standard HTTP libraries instead.

Working examples are in the `examples/` directory: `examples/python/`, `examples/go/`, `examples/javascript/`.

## Python

This example uses the `requests` library. Install it with `pip install requests`.

```python
import requests

BASE = "http://localhost:4566"

# --- Resource Group ---
resp = requests.put(
    f"{BASE}/subscriptions/sub1/resourcegroups/python-rg",
    json={"location": "eastus", "tags": {"sdk": "python"}},
)
rg = resp.json()
print(f"Resource Group: {rg['name']} ({rg['location']})")

# --- Key Vault ---
requests.put(
    f"{BASE}/keyvault/myvault/secrets/db-password",
    json={"value": "super-secret-123"},
)
secret = requests.get(f"{BASE}/keyvault/myvault/secrets/db-password").json()
print(f"Secret: {secret['value']}")

# --- Blob Storage ---
requests.put(f"{BASE}/blob/myaccount/data")  # create container
requests.put(
    f"{BASE}/blob/myaccount/data/config.json",
    json={"database": "postgres://localhost:5432/mydb"},
)
blob = requests.get(f"{BASE}/blob/myaccount/data/config.json").json()
print(f"Blob: {blob}")

# --- Cosmos DB ---
requests.post(
    f"{BASE}/cosmosdb/myaccount/dbs/app/colls/users/docs",
    json={"id": "user1", "name": "Mo", "role": "admin"},
)
doc = requests.get(f"{BASE}/cosmosdb/myaccount/dbs/app/colls/users/docs/user1").json()
print(f"Doc: {doc['name']} ({doc['role']})")
```

Run:

```bash
cd examples/python
pip install -r requirements.txt
python example.py
```

## Go

This example uses the standard `net/http` package with no third-party dependencies.

```go
package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"
)

const baseURL = "http://localhost:4566"

func main() {
    // --- Resource Group ---
    body, _ := json.Marshal(map[string]interface{}{
        "location": "eastus",
        "tags":     map[string]string{"env": "local"},
    })
    req, _ := http.NewRequest("PUT",
        baseURL+"/subscriptions/sub1/resourcegroups/go-rg",
        bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    data, _ := io.ReadAll(resp.Body)
    fmt.Printf("Resource Group: %s\n", string(data))

    // --- Key Vault ---
    secret, _ := json.Marshal(map[string]string{"value": "my-api-key-123"})
    kvReq, _ := http.NewRequest("PUT",
        baseURL+"/keyvault/myvault/secrets/api-key",
        bytes.NewReader(secret))
    kvReq.Header.Set("Content-Type", "application/json")
    kvResp, err := http.DefaultClient.Do(kvReq)
    if err != nil {
        log.Fatal(err)
    }
    defer kvResp.Body.Close()
    kvData, _ := io.ReadAll(kvResp.Body)
    fmt.Printf("Secret: %s\n", string(kvData))

    // --- Blob Storage ---
    putReq, _ := http.NewRequest("PUT",
        baseURL+"/blob/myaccount/data", nil)
    http.DefaultClient.Do(putReq)

    blobBody := []byte(`{"key": "value"}`)
    blobReq, _ := http.NewRequest("PUT",
        baseURL+"/blob/myaccount/data/config.json",
        bytes.NewReader(blobBody))
    blobReq.Header.Set("Content-Type", "application/json")
    http.DefaultClient.Do(blobReq)

    getResp, _ := http.DefaultClient.Get(baseURL + "/blob/myaccount/data/config.json")
    defer getResp.Body.Close()
    blobData, _ := io.ReadAll(getResp.Body)
    fmt.Printf("Blob: %s\n", string(blobData))
}
```

Run:

```bash
cd examples/go
go run main.go
```

## JavaScript

This example uses the built-in `fetch` API in Node.js 18+ or any modern browser.

```javascript
const BASE_URL = "http://localhost:4566";

async function main() {
  // --- Resource Group ---
  const rgResp = await fetch(
    `${BASE_URL}/subscriptions/sub1/resourcegroups/js-rg`,
    {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ location: "eastus", tags: { env: "local" } }),
    }
  );
  const rg = await rgResp.json();
  console.log(`Resource Group: ${rg.name} (${rg.location})`);

  // --- Key Vault ---
  await fetch(`${BASE_URL}/keyvault/myvault/secrets/js-secret`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ value: "super-secret-from-js" }),
  });
  const secret = await (
    await fetch(`${BASE_URL}/keyvault/myvault/secrets/js-secret`)
  ).json();
  console.log(`Secret: ${secret.value}`);

  // --- Blob Storage ---
  await fetch(`${BASE_URL}/blob/myaccount/mycontainer`, { method: "PUT" });
  await fetch(`${BASE_URL}/blob/myaccount/mycontainer/hello.txt`, {
    method: "PUT",
    headers: { "Content-Type": "text/plain" },
    body: "Hello from JavaScript!",
  });
  const blob = await (
    await fetch(`${BASE_URL}/blob/myaccount/mycontainer/hello.txt`)
  ).text();
  console.log(`Blob: ${blob}`);

  // --- Cosmos DB ---
  await fetch(`${BASE_URL}/cosmosdb/myaccount/dbs/app/colls/users/docs`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ id: "user1", name: "Alice", role: "admin" }),
  });
  const doc = await (
    await fetch(`${BASE_URL}/cosmosdb/myaccount/dbs/app/colls/users/docs/user1`)
  ).json();
  console.log(`Doc: ${doc.name} (${doc.role})`);
}

main().catch(console.error);
```

Run:

```bash
cd examples/javascript
node example.js
```

## Why not use the official Azure SDKs?

The official Azure SDKs (`azure-sdk-for-python`, `azure-sdk-for-go`, `@azure/` npm packages) enforce HTTPS and validate OAuth tokens. miniblue's HTTP endpoint is simpler to call with standard HTTP clients.

If your application code already uses the Azure SDK, you have two options:

1. Use Terraform or Pulumi to provision resources. Both handle the HTTPS requirement via miniblue's TLS endpoint on port 4567.
2. Use HTTP calls in tests. Point your test helpers at `http://localhost:4566` and use `requests`, `fetch`, or `net/http`.

## API cheat sheet

| Operation | Method | URL |
|-----------|--------|-----|
| Create resource group | `PUT` | `/subscriptions/{sub}/resourcegroups/{name}` |
| List resource groups | `GET` | `/subscriptions/{sub}/resourcegroups` |
| Set secret | `PUT` | `/keyvault/{vault}/secrets/{name}` |
| Get secret | `GET` | `/keyvault/{vault}/secrets/{name}` |
| Create blob container | `PUT` | `/blob/{account}/{container}` |
| Upload blob | `PUT` | `/blob/{account}/{container}/{blob}` |
| Download blob | `GET` | `/blob/{account}/{container}/{blob}` |
| Create CosmosDB doc | `POST` | `/cosmosdb/{account}/dbs/{db}/colls/{coll}/docs` |
| Get CosmosDB doc | `GET` | `/cosmosdb/{account}/dbs/{db}/colls/{coll}/docs/{id}` |
