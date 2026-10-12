# Virtual Machines

miniblue emulates Azure Virtual Machines (`Microsoft.Compute/virtualMachines`) via ARM endpoints. When Docker is available on the host, each VM is backed by a real Docker container on its own per-VM network. Without Docker the ARM lifecycle still works (create, update, power state, instance view, extensions); operations that need a running container (`runCommand`, `logs`, deploying services) return `409 DockerUnavailable`.

A VM is more than a single container. You can deploy named service containers onto it, stream their logs, run commands inside it, open an interactive shell and attach managed identities for token attestation.

The ARM surface is shaped for the `azurerm` provider's idempotency checks: the VM you `PUT` is replayed on `GET`, with the defaults Azure fills in (see [ARM fidelity](#arm-fidelity)).

## Docker detection

miniblue probes for the `docker` CLI at startup and runs `docker info`. The log shows:

```
[vm] Docker is available
```

or it falls back to stub mode when the CLI is absent or the daemon is unreachable. Set `MINIBLUE_DISABLE_DOCKER=1` to force stub mode.

## Image mapping

VMs boot a Linux container image. miniblue resolves the image in this order:

1. `properties.miniblue.image` is an explicit container image and the recommended escape hatch (for example `nginx:alpine`).
2. `properties.storageProfile.imageReference` is mapped through an alias table.
3. With no image reference at all, the default is `ubuntu:24.04`.

| `imageReference` contains | Container image |
|---------------------------|-----------------|
| `24.04` / `2404` / `24_04` | `ubuntu:24.04` |
| `22.04` / `2204` / `22_04` | `ubuntu:22.04` |
| `ubuntu` (any other) | `ubuntu:24.04` |
| `debian` | `debian:stable-slim` |

With Docker, an `imageReference` that is present but cannot be mapped (for example a Windows image) fails with `400`, so a typo does not silently boot the wrong OS. Set `properties.miniblue.image` to bypass the table. In stub mode there is no container to boot, so the VM is accepted as-is and no image is recorded.

## API endpoints

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `.../Microsoft.Compute/virtualMachines/{name}` | Create or update a VM (`201` on create, `200` on update) |
| `PATCH` | `.../Microsoft.Compute/virtualMachines/{name}` | Merge-update a VM (`tags`, `properties`, `identity`, ...) |
| `GET` | `.../Microsoft.Compute/virtualMachines/{name}` | Get a VM (live power state; `$expand=instanceView` supported) |
| `DELETE` | `.../Microsoft.Compute/virtualMachines/{name}` | Delete a VM, its services, extensions and network (`204`) |
| `GET` | `.../Microsoft.Compute/virtualMachines` | List VMs in a resource group |
| `GET` | `/subscriptions/{sub}/providers/Microsoft.Compute/virtualMachines` | List VMs across the subscription |
| `GET` | `.../virtualMachines/{name}/instanceView` | Power state and provisioning status |
| `POST` | `.../virtualMachines/{name}/start` | Start the VM and its services |
| `POST` | `.../virtualMachines/{name}/powerOff` | Stop the VM and its services |
| `POST` | `.../virtualMachines/{name}/restart` | Restart the VM and its services |
| `POST` | `.../virtualMachines/{name}/deallocate` | Deallocate the VM (stopped, no compute) |
| `POST` | `.../virtualMachines/{name}/redeploy` | Redeploy; the VM ends up running |
| `GET` | `.../virtualMachines/{name}/extensions` | List extensions |
| `PUT` `PATCH` `GET` `DELETE` | `.../virtualMachines/{name}/extensions/{extension}` | Manage an extension (`DELETE` returns `202`) |
| `POST` | `.../virtualMachines/{name}/runCommand` | Run a command inside the VM |
| `GET` | `.../virtualMachines/{name}/logs` | Read service logs (`?service=`, `?tail=N`, `?follow=true`) |
| `GET` | `.../virtualMachines/{name}/services` | List deployed services |
| `PUT` | `.../virtualMachines/{name}/services/{service}` | Deploy or replace a service |
| `GET` | `.../virtualMachines/{name}/services/{service}` | Get a service |
| `DELETE` | `.../virtualMachines/{name}/services/{service}` | Remove a service |

Paths are abbreviated; the full prefix is `/subscriptions/{sub}/resourceGroups/{rg}/providers`. Resource paths are case-insensitive.

## Create a VM

```bash
azlocal group create --name myRG --location eastus
azlocal vm create --resource-group myRG --name web01 --image ubuntu:24.04
```

or via the ARM API:

```bash
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Compute/virtualMachines/web01" \
  -H "Content-Type: application/json" \
  -d '{
    "location": "eastus",
    "properties": {
      "storageProfile": {
        "imageReference": { "publisher": "Canonical", "offer": "ubuntu", "sku": "24_04-lts" }
      }
    }
  }'
```

Response (`201 Created`) follows the ARM envelope with `provisioningState: Succeeded` and a `miniblue.powerState` field. When Docker is available the create starts a keep-alive container on a per-VM network. A VM whose container fails to start fails fast with `409 ContainerStartFailed` and persists nothing, unlike ACI which falls back to a stub. The resource group must exist (`404 ResourceGroupNotFound` otherwise). `adminPassword` is never returned.

## ARM fidelity

So Terraform sees no drift after `apply`, a `GET` returns what was `PUT` plus the values Azure fills in:

- `hardwareProfile.vmSize` defaults to `Standard_B1s`; `vmId` is deterministic per VM.
- `storageProfile.osDisk` gains `diskSizeGB` (30, or 127 for a Windows image), a name of the form `<vm>_OsDisk_<hash>` and a `managedDisk.id`.
- The OS disk is persisted in the [managed disk](managed-disks.md) store with `managedBy` set to the VM. A disk you pass via `osDisk.managedDisk.id` or `storageProfile.dataDisks` is left alone and only back-referenced.
- NICs in `networkProfile.networkInterfaces` get `properties.virtualMachine` set; references are released on update and delete.
- SSH keys, `additionalCapabilities` and the other `osProfile`/`securityProfile` sections are replayed verbatim. An empty `additionalCapabilities` is dropped, as in Azure.
- `PATCH` replaces top-level fields (`tags`, `identity`, ...) and merges `properties` one level deep. `tags` default to `{}`.
- Deleting the resource group cascades to the VM, its OS disk, NIC back-references and extensions.

## Power actions

```bash
azlocal vm stop       --resource-group myRG --name web01
azlocal vm start      --resource-group myRG --name web01
azlocal vm restart    --resource-group myRG --name web01
azlocal vm deallocate --resource-group myRG --name web01
azlocal vm redeploy   --resource-group myRG --name web01
```

Power actions return `200` with the updated VM (real Azure returns `202`; miniblue completes synchronously). They cascade to every service container on the VM. With Docker, `powerState` on a `GET` is refreshed live from `docker inspect`, and a `deallocated` VM stays deallocated while its container is stopped. Without Docker the recorded state is authoritative, so `GET .../instanceView` reflects the last action.

## Extensions

```bash
azlocal vm extension set --resource-group myRG --vm-name web01 \
  --name customScript --publisher Microsoft.Azure.Extensions \
  --type CustomScript --settings '{"commandToExecute":"echo ok"}'
azlocal vm extension list --resource-group myRG --vm-name web01
```

Extensions are stored and round-tripped, not executed. `protectedSettings` are accepted but never returned. `PUT` returns `201` on create and `200` on update; `DELETE` returns `202`.

## Deploy services

A service is a named container that runs on the VM's network with host-published ports.

```bash
azlocal vm deploy --resource-group myRG --name web01 \
  --service api --image nginx:alpine --ports 8080:80 \
  --env LOG_LEVEL=debug --env REGION=local
```

Deploying a service whose host port clashes with a sibling returns `409 PortConflict`. Re-deploying the same service name replaces the existing container. List and remove services with:

```bash
azlocal vm services       --resource-group myRG --name web01
azlocal vm service-delete --resource-group myRG --name web01 --service api
```

## Logs

```bash
# combined, labelled view of every service
azlocal vm logs --resource-group myRG --name web01

# a single service, last 100 lines
azlocal vm logs --resource-group myRG --name web01 --service api --tail 100

# live stream
azlocal vm logs --resource-group myRG --name web01 --follow
```

Combined output prefixes each line with `[service]`. Streaming uses chunked `text/plain` and ends cleanly when the client disconnects.

## Run commands and SSH

```bash
# one-off command, mirrors Azure runCommand
azlocal vm run-command --resource-group myRG --name web01 --command "uname -a"

# interactive shell (docker exec -it, /bin/bash with /bin/sh fallback)
azlocal vm ssh --resource-group myRG --name web01
```

`runCommand` captures stdout, stderr and the exit code. Both require a running container and return `409 VMNotRunning` or `409 DockerUnavailable` otherwise. `vm ssh` shells out to `docker exec` directly, so it needs Docker on the same host as the CLI.

## Managed identity attestation

User-assigned managed identities live under `Microsoft.ManagedIdentity/userAssignedIdentities` and can be attached to a VM. A workload inside the VM (or its services) then obtains a token with no code changes through the standard `IDENTITY_ENDPOINT` / `IDENTITY_HEADER` protocol that Azure SDK credential chains already probe.

```bash
azlocal identity create --resource-group myRG --name app-id
azlocal vm identity-assign --resource-group myRG --name web01 --identity app-id

# from inside the VM, the SDK or a curl call hits the token endpoint
curl "http://localhost:4566/metadata/identity/oauth2/token?resource=https://management.azure.com/"
```

Tokens carry `xms_mirid` and VM claims and are verifiable via `POST /metadata/identity/introspect`. The signature is a clear mock (`alg: none`); validation is by store lookup, not by trusting token contents. Remove an assignment with `azlocal vm identity-remove`.

## azlocal command reference

```
azlocal vm create|update|list|show|delete
azlocal vm get-instance-view
azlocal vm start|stop|restart|deallocate|redeploy
azlocal vm deploy|services|service-delete
azlocal vm logs            [--service NAME] [--tail N] [--follow]
azlocal vm run-command     --command "..."
azlocal vm ssh
azlocal vm identity-assign|identity-remove --identity NAME
azlocal vm extension set|update|show|list|delete --vm-name VM
azlocal identity create|list|show|update|delete
```

`azlocal vm create --image` takes a container image (`properties.miniblue.image`), not an Azure image URN.

## Limitations

- The VM is a Linux container, not a real virtual machine. There is no kernel isolation, no Windows images in Docker mode and no cloud-init.
- The keep-alive container runs `sleep`; extensions, custom-data scripts and boot diagnostics are stored but not executed.
- Without Docker, ARM lifecycle and power state work on recorded state, but `runCommand`, `logs`, service deploy and `vm ssh` return `409 DockerUnavailable`.
- Only user-assigned identities can be attached to a VM; `SystemAssigned` is rejected.
- Container and network names are derived from the resource group and VM name, so those must be valid Docker identifiers.
