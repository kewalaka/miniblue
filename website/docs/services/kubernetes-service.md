# Kubernetes Service (AKS)

miniblue emulates Azure Kubernetes Service via the `Microsoft.ContainerService` ARM endpoints. Two backends are available:

- **Stub backend (default).** Cluster create, get, list, and delete operations update miniblue's in-memory store. `listClusterAdminCredential` returns a syntactically valid kubeconfig pointing at a sentinel host, which works for `terraform plan`, `terraform apply`, `az aks list`, and IaC iteration.
- **Real backend with k3s in Docker.** When `AKS_BACKEND=k3s` is set and Docker is reachable, each cluster create launches a `rancher/k3s` container on a dynamic localhost port. `listClusterAdminCredential` returns the real kubeconfig so `kubectl` can deploy workloads.

Both backends implement the same ARM contract, so Terraform, Bicep, and `az` behave the same way against either backend.

## API endpoints

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.ContainerService/managedClusters/{name}` | Create or update cluster |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.ContainerService/managedClusters/{name}` | Get cluster |
| `DELETE` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.ContainerService/managedClusters/{name}` | Delete cluster (also tears down the k3s container in real mode) |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.ContainerService/managedClusters` | List clusters in resource group |
| `GET` | `/subscriptions/{sub}/providers/Microsoft.ContainerService/managedClusters` | List clusters in subscription (used by `az aks list`) |
| `GET` | `.../managedClusters/{name}/agentPools` | List node pools |
| `GET` | `.../managedClusters/{name}/agentPools/{poolName}` | Get a specific node pool |
| `POST` | `.../managedClusters/{name}/listClusterAdminCredential` | Returns base64-encoded kubeconfig |
| `POST` | `.../managedClusters/{name}/listClusterUserCredential` | Same shape; same kubeconfig |

miniblue validates cluster names against Azure's rule (`^[a-zA-Z0-9][-_a-zA-Z0-9]{0,62}$`) and rejects invalid names with `400 BadRequest`.

## Stub mode

Stub mode is the default and does not require Docker. It returns ARM responses that let Terraform plan, apply, and destroy resources. The kubeconfig points at `https://miniblue-aks.invalid:443`, so `kubectl` will not connect to it.

```bash
curl -X PUT 'http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.ContainerService/managedClusters/my-cluster' \
  -H 'Content-Type: application/json' \
  -d '{
    "location": "eastus",
    "identity": {"type": "SystemAssigned"},
    "properties": {
      "dnsPrefix": "myprefix",
      "agentPoolProfiles": [
        {"name": "default", "count": 1, "vmSize": "Standard_DS2_v2", "mode": "System"}
      ]
    }
  }'
```

## Real backend

Set `AKS_BACKEND=k3s` (or `MINIBLUE_AKS_REAL=1`) and run miniblue with Docker reachable.

```bash
# Binary install (recommended for local dev)
AKS_BACKEND=k3s ./bin/miniblue

# Or via the `full` Docker image variant (the default scratch image has no
# docker CLI to shell out to; build the `full` target for real backends):
docker build --target=full -t miniblue:full .
docker run -v /var/run/docker.sock:/var/run/docker.sock \
  -p 4566:4566 -p 443:4567 -p 4567:4567 \
  -e AKS_BACKEND=k3s miniblue:full
```

On startup, miniblue pulls the `rancher/k3s` image, about 200 MB, once so the first cluster create does not block. Subsequent creates take 5 to 10 seconds.

```bash
azlocal aks create --resource-group myRG --name my-cluster --node-count 2
azlocal aks get-credentials --resource-group myRG --name my-cluster
kubectl get nodes
kubectl create deployment hello --image=nginx
```

`azlocal aks get-credentials` merges into `~/.kube/config` by default, matching `az aks get-credentials`, and preserves existing contexts such as `docker-desktop`, GKE, and AKS. Use `--overwrite-existing` to replace the file or `--file -` to write to stdout.

## Lifecycle and teardown

In real mode, miniblue tracks every k3s container it spawns and cleans them up via several paths:

- **Explicit cluster delete.** Deleting a cluster through the ARM endpoint, `azlocal aks delete`, or `terraform destroy` removes the k3s container immediately.
- **Resource group cascade delete.** Removing the parent resource group removes every k3s container backing a cluster in that resource group.
- **Graceful miniblue shutdown.** On `SIGTERM`, `SIGINT`, or `docker stop`, miniblue stops and removes all running k3s containers for known clusters before exiting.
- **Forceful kill.** After `SIGKILL`, an OOM kill, or a host crash, containers keep running. The next miniblue start with `AKS_BACKEND=k3s` removes any orphaned `miniblue-aks-*` containers that no longer match a cluster in the store.
- **Persistence enabled.** With `PERSISTENCE=1`, miniblue keeps surviving k3s containers whose clusters remain in the persisted store across restarts. Existing kubeconfigs keep working because the host port is stored in the persisted backend handle.

To remove orphaned containers manually:

```bash
docker ps --filter name=miniblue-aks- -q | xargs -r docker rm -f
```

## Cross-cluster naming

miniblue distinguishes clusters that share a short name across resource groups, such as `dev/k1` and `prod/k1`, by adding a hash suffix to the Docker container name (`miniblue-aks-k1-<8charHash>`).

## Real backend constraints

- Docker, OrbStack, or Rancher Desktop must be reachable from the miniblue process.
- The default `:latest` Docker image uses `FROM scratch` and has no Docker CLI. Use the `:full` build target or run miniblue as a host binary to use the real backend.
- k3s is API-compatible with vanilla Kubernetes, but it uses Flannel CNI by default, runs SQLite instead of etcd in single-node mode, and disables Traefik for faster startup.
