# AKS example

This example provisions a minimal `azurerm_kubernetes_cluster` against miniblue.

## Prerequisites

```bash
./bin/miniblue
export SSL_CERT_FILE=~/.miniblue/cert.pem
```

## Apply

```bash
terraform init
terraform apply -auto-approve
```

The cluster appears in `azlocal aks list --resource-group aks-example-rg` and can be removed with `terraform destroy`.

## Two backends

By default, AKS in miniblue is stub-only. The cluster exists in the ARM API so Terraform plans, applies, and destroys it, but `kubectl` cannot connect using the returned kubeconfig because no Kubernetes cluster is running.

To run a real Kubernetes cluster behind the AKS resource:

```bash
# Binary (recommended for local dev):
AKS_BACKEND=k3s ./bin/miniblue

# Or via the full Docker image (default `latest` is scratch and has no
# docker CLI; use the `full` target for real backends):
docker build --target=full -t miniblue:full .
docker run -v /var/run/docker.sock:/var/run/docker.sock \
  -p 4566:4566 -p 443:4567 -p 4567:4567 \
  -e AKS_BACKEND=k3s miniblue:full
```

miniblue starts a `rancher/k3s` container per cluster create (about 5 to 10 seconds, plus a one-off ~200 MB image pull on first run) and returns a working kubeconfig:

```bash
azlocal aks get-credentials --resource-group aks-example-rg --name miniblue-aks
kubectl get nodes
kubectl create deployment hello --image=nginx
```

This backend requires Docker, OrbStack, or Rancher Desktop running on the host. Expect a short delay of a few seconds between cluster creation returning and the node reporting `Ready` in `kubectl get nodes`.

### Cleanup

miniblue removes the k3s container backing each AKS resource on every normal exit path:

| Trigger | What happens |
|---|---|
| `terraform destroy` / `azlocal aks delete` / direct `DELETE` on the cluster | container removed immediately |
| Resource group cascade delete | every cluster's container in that RG removed |
| miniblue `SIGTERM` / `SIGINT` / `docker stop` | every running k3s container torn down before miniblue exits |
| miniblue `SIGKILL` / OOM / host crash | containers persist; the next miniblue start with `AKS_BACKEND=k3s` removes any orphaned containers, or keeps them if `PERSISTENCE=1` shows a stored cluster still owns them |

To remove leftover containers manually:

```bash
docker ps --filter name=miniblue-aks- -q | xargs -r docker rm -f
```
