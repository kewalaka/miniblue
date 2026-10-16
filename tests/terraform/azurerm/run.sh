#!/usr/bin/env bash
# azurerm acceptance runner: run.sh <fixture>
#
# <fixture> is a directory under tests/terraform/azurerm/ or the path of any Terraform root
# module that carries a .terraform.lock.hcl. It starts a private miniblue, then runs
#   init -> apply -> plan (must report no drift) -> apply (must be a no-op) -> destroy
#
# Needs terraform, curl and a miniblue binary (MINIBLUE_BIN, default <repo>/miniblue).
# Use Linux or WSL2: Go ignores SSL_CERT_FILE on Windows, so it cannot trust miniblue's cert.
#
# A fixture may ship an acc.env next to its .tf files to set:
#   TLS_PORT   miniblue HTTPS port (443 when the provider dials a *.vault.azure.net host)
#   ACC_HOSTS  space-separated names that must resolve to 127.0.0.1
#
# Optional environment: PORT (4566), TLS_PORT (4567), ACC_STEP_TIMEOUT (600s per step),
# ACC_KEEP=1 to keep the work dir, ACC_SETUP_SYSTEM=1 to let this script use sudo to add
# /etc/hosts entries and lower ip_unprivileged_port_start (CI sets it).
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../../.." && pwd)

die() { echo "FAIL: $*" >&2; exit 1; }
step() { printf '\n== %s\n' "$*"; }
sys() { if [ "$(id -u)" = 0 ]; then "$@"; else sudo -n "$@"; fi; }

name=${1:?usage: run.sh <fixture>}
if [ -d "$here/$name" ]; then target=$here/$name; elif [ -d "$name" ]; then target=$(cd "$name" && pwd); else die "no such fixture: $name"; fi
[ -f "$target/.terraform.lock.hcl" ] || die "$target has no .terraform.lock.hcl (see tests/terraform/azurerm/README.md)"
[ -f "$target/acc.env" ] && . "$target/acc.env"

PORT=${PORT:-4566}
TLS_PORT=${TLS_PORT:-4567}
bin=${MINIBLUE_BIN:-$repo/miniblue}
[ -x "$bin" ] || die "no miniblue binary at $bin (go build -o miniblue ./cmd/miniblue, or set MINIBLUE_BIN)"

for ca in /etc/ssl/certs/ca-certificates.crt /etc/pki/tls/certs/ca-bundle.crt; do [ -f "$ca" ] && break; done
[ -f "$ca" ] || die "no system CA bundle found"

if [ "$TLS_PORT" -lt 1024 ] && [ "$(id -u)" != 0 ] &&
  [ "$(cat /proc/sys/net/ipv4/ip_unprivileged_port_start 2>/dev/null || echo 1024)" -gt "$TLS_PORT" ]; then
  [ -n "${ACC_SETUP_SYSTEM:-}" ] || die "port $TLS_PORT is privileged: run as root, or sudo sysctl net.ipv4.ip_unprivileged_port_start=$TLS_PORT, or set ACC_SETUP_SYSTEM=1"
  sys sysctl -q "net.ipv4.ip_unprivileged_port_start=$TLS_PORT"
fi
for host in ${ACC_HOSTS:-}; do
  getent hosts "$host" >/dev/null && continue
  [ -n "${ACC_SETUP_SYSTEM:-}" ] || die "add '127.0.0.1 $host' to /etc/hosts, or set ACC_SETUP_SYSTEM=1"
  echo "127.0.0.1 $host" | sys tee -a /etc/hosts >/dev/null
done

work=$(mktemp -d)
mbpid=
cleanup() {
  local rc=$?
  [ -n "$mbpid" ] && kill "$mbpid" 2>/dev/null || true
  if [ "$rc" -ne 0 ] && [ -f "$work/miniblue.log" ]; then
    printf '\n== miniblue log (last 100 lines)\n'
    tail -n 100 "$work/miniblue.log"
  fi
  if [ -n "${ACC_KEEP:-}" ]; then echo "kept $work"; else rm -rf "$work"; fi
}
trap cleanup EXIT

step "start miniblue (http :$PORT, https :$TLS_PORT)"
PORT=$PORT TLS_PORT=$TLS_PORT MINIBLUE_DISABLE_DOCKER=1 LOCAL_AZURE_CERT_DIR="$work/cert" \
  "$bin" >"$work/miniblue.log" 2>&1 &
mbpid=$!
for _ in $(seq 60); do
  curl -sf "http://localhost:$PORT/health" >/dev/null 2>&1 && break
  kill -0 "$mbpid" 2>/dev/null || die "miniblue exited early"
  sleep 0.5
done
curl -sf "http://localhost:$PORT/health" >/dev/null || die "miniblue did not become healthy"

# Replaces the system trust store for Go, so keep the system CAs for the provider registry.
cat "$ca" "$work/cert/cert.pem" >"$work/ca.pem"
export SSL_CERT_FILE=$work/ca.pem

export ARM_METADATA_HOSTNAME="localhost:$TLS_PORT"
export ARM_SUBSCRIPTION_ID=00000000-0000-0000-0000-000000000000
export ARM_TENANT_ID=00000000-0000-0000-0000-000000000001
export ARM_CLIENT_ID=miniblue ARM_CLIENT_SECRET=miniblue
export ARM_RESOURCE_PROVIDER_REGISTRATIONS=none
export GO_AZURE_SDK_SKIP_POLLING_DELAY=true
export TF_IN_AUTOMATION=1 TF_INPUT=0 TF_CLI_ARGS=-no-color

mkdir "$work/tf"
cp "$target"/*.tf "$target/.terraform.lock.hcl" "$work/tf/"
tf() { timeout "${ACC_STEP_TIMEOUT:-600}" terraform -chdir="$work/tf" "$@"; }

step "terraform init"
tf init -lockfile=readonly

step "terraform apply"
tf apply -auto-approve

step "terraform plan must report no drift"
rc=0
tf plan -detailed-exitcode || rc=$?
[ "$rc" -eq 0 ] || die "plan -detailed-exitcode returned $rc (0 = clean, 2 = drift)"

step "second terraform apply must be a no-op"
tf apply -auto-approve | tee "$work/apply2.log"
grep -q '^No changes\.' "$work/apply2.log" || die "second apply changed something"

step "terraform destroy"
tf destroy -auto-approve

printf '\nPASS %s\n' "$name"
