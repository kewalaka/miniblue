#!/usr/bin/env bash
# Acceptance test: Microsoft.Authorization/policyDefinitions through the azapi provider.
#
# Needs a running miniblue, terraform, curl and python3. SSL_CERT_FILE must point at
# miniblue's cert (<LOCAL_AZURE_CERT_DIR>/cert.pem) so Terraform and curl trust it.
set -euo pipefail
cd "$(dirname "$0")"

: "${SSL_CERT_FILE:?set SSL_CERT_FILE to the miniblue cert.pem}"

MINIBLUE_URL=${MINIBLUE_URL:-https://localhost:4567}
SUB=a1b2c3d4-0000-4000-8000-0000000000aa
NAME=Miniblue-Deny-Locations
API=2023-04-01
WANT_ID="/subscriptions/$SUB/providers/Microsoft.Authorization/policyDefinitions/$NAME"

export TF_IN_AUTOMATION=1 TF_INPUT=0 TF_CLI_ARGS=-no-color
export TF_VAR_miniblue_endpoint=$MINIBLUE_URL TF_VAR_subscription_id=$SUB TF_VAR_policy_name=$NAME

BODY=$(mktemp)
trap 'rm -f "$BODY"' EXIT

step() { printf '\n== %s\n' "$*"; }
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

# get <path-with-casing-to-test> -> prints HTTP status, body in $BODY
get() { curl -sS -o "$BODY" -w '%{http_code}' --cacert "$SSL_CERT_FILE" "$MINIBLUE_URL$1?api-version=$API"; }

# json_ok <python expression over d> asserts on the last response body
json_ok() { python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); assert eval(sys.argv[2]), d' "$BODY" "$1" || fail "body check: $1"; }

step "terraform init"
terraform init -upgrade=false

step "terraform apply"
terraform apply -auto-approve
[ "$(terraform output -raw policy_definition_id)" = "$WANT_ID" ] || fail "output id != $WANT_ID"

step "terraform plan must report no drift"
rc=0
terraform plan -detailed-exitcode || rc=$?
[ "$rc" -eq 0 ] || fail "plan -detailed-exitcode returned $rc (want 0)"

step "GET with mangled URL casing"
for path in \
  "/SUBSCRIPTIONS/${SUB^^}/PROVIDERS/MICROSOFT.AUTHORIZATION/POLICYDEFINITIONS/${NAME^^}" \
  "/Subscriptions/${SUB^^}/Providers/microsoft.authorization/policydefinitions/${NAME,,}" \
  "/subscriptions/$SUB/providers/Microsoft.Authorization/policyDefinitions/$NAME"; do
  code=$(get "$path")
  [ "$code" = 200 ] || fail "GET $path -> $code (want 200)"
  json_ok "d['id'] == '$WANT_ID' and d['name'] == '$NAME' and d['type'] == 'Microsoft.Authorization/policyDefinitions'"
  json_ok "d['properties']['policyRule']['then']['effect'] == 'deny'"
done

step "list at subscription scope with mangled casing"
code=$(get "/SUBSCRIPTIONS/${SUB^^}/providers/MICROSOFT.AUTHORIZATION/POLICYDEFINITIONS")
[ "$code" = 200 ] || fail "list -> $code (want 200)"
json_ok "[v['id'] for v in d['value']] == ['$WANT_ID']"

step "terraform destroy"
terraform destroy -auto-approve

step "GET after destroy must be 404"
for path in "$WANT_ID" "${WANT_ID^^}"; do
  code=$(get "$path")
  [ "$code" = 404 ] || fail "GET $path after destroy -> $code (want 404)"
done

printf '\nPASS\n'
