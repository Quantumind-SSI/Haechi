#!/usr/bin/env bash
#
# Provisions or updates the Haechi control-plane platform in Azure.
#
# This is the platform stack: PostgreSQL, Keycloak, OpenBao, OPA and the
# control API. It is separate from deploy.sh, which provisions the marketing
# website, and the two share nothing but a subscription.
#
# Idempotent: Bicep is declarative, so re-running converges the resource group
# rather than duplicating it.
#
# Required environment:
#   AZURE_SUBSCRIPTION_ID     target subscription
#   HAECHI_PLAT_DB_PASSWORD   PostgreSQL administrator password, 16+ characters
#   HAECHI_PLAT_KC_PASSWORD   Keycloak bootstrap admin password, 16+ characters
#   HAECHI_PLAT_BAO_TOKEN     OpenBao development root token, 16+ characters
#
# Optional environment:
#   HAECHI_ENVIRONMENT        staging (default)
#   HAECHI_LOCATION           uksouth (default)
#   HAECHI_ALLOWED_IPS        comma separated IPv4 allowlist. Defaults to the
#                             address this machine presents to Azure.
#   HAECHI_CONTROL_API_IMAGE  image to run; defaults to the seed below
#   HAECHI_WHATIF             set to 1 to preview changes and exit
#
# The seed image exists only so the first deployment can create the container
# app before any application image has been pushed.

set -euo pipefail

readonly SEED_IMAGE="mcr.microsoft.com/k8se/quickstart:latest"
readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

fail() {
  printf 'error: %s\n' "$1" >&2
  exit 1
}

require_env() {
  local name="$1" min="${2:-1}"
  local value="${!name:-}"
  [[ -n "${value}" ]] || fail "${name} is required"
  (( ${#value} >= min )) || fail "${name} must be at least ${min} characters"
}

require_env AZURE_SUBSCRIPTION_ID
require_env HAECHI_PLAT_DB_PASSWORD 16
require_env HAECHI_PLAT_KC_PASSWORD 16
require_env HAECHI_PLAT_BAO_TOKEN 16

environment="${HAECHI_ENVIRONMENT:-staging}"
location="${HAECHI_LOCATION:-uksouth}"
image="${HAECHI_CONTROL_API_IMAGE:-${SEED_IMAGE}}"
resource_group="rg-haechi-${environment}-platform-${location}"

command -v az >/dev/null || fail "the Azure CLI is required"

# Resolve the allowlist. Defaulting to this machine's public address is what
# makes a single-developer environment usable without hand-editing a file, and
# an empty allowlist is refused rather than silently opening the ingress.
if [[ -n "${HAECHI_ALLOWED_IPS:-}" ]]; then
  IFS=',' read -r -a allowed_ips <<< "${HAECHI_ALLOWED_IPS}"
else
  printf 'Resolving this machine public address...\n'
  detected="$(curl -fsS --max-time 10 https://api.ipify.org || true)"
  [[ -n "${detected}" ]] || fail "could not detect a public address; set HAECHI_ALLOWED_IPS"
  allowed_ips=("${detected}")
fi

for ip in "${allowed_ips[@]}"; do
  [[ "${ip}" =~ ^[0-9]{1,3}(\.[0-9]{1,3}){3}$ ]] || fail "not an IPv4 address: ${ip}"
done

ip_json="$(printf '%s\n' "${allowed_ips[@]}" | python3 -c 'import json,sys; print(json.dumps([l.strip() for l in sys.stdin if l.strip()]))')"

printf 'Subscription:   %s\n' "${AZURE_SUBSCRIPTION_ID}"
printf 'Environment:    %s\n' "${environment}"
printf 'Resource group: %s\n' "${resource_group}"
printf 'Allowed IPs:    %s\n' "${allowed_ips[*]}"
printf 'Control image:  %s\n\n' "${image}"

az account set --subscription "${AZURE_SUBSCRIPTION_ID}"

az group create \
  --name "${resource_group}" \
  --location "${location}" \
  --output none
printf 'Resource group ready.\n'

deploy_args=(
  --resource-group "${resource_group}"
  --template-file "${SCRIPT_DIR}/platform.bicep"
  --parameters "${SCRIPT_DIR}/platform.parameters.${environment}.json"
  --parameters
    environmentName="${environment}"
    location="${location}"
    controlApiImage="${image}"
    allowedIpAddresses="${ip_json}"
    databaseAdministratorPassword="${HAECHI_PLAT_DB_PASSWORD}"
    keycloakAdminPassword="${HAECHI_PLAT_KC_PASSWORD}"
    openBaoRootToken="${HAECHI_PLAT_BAO_TOKEN}"
)

if [[ "${HAECHI_WHATIF:-0}" == "1" ]]; then
  printf 'Previewing changes (no resources will be created).\n\n'
  az deployment group what-if "${deploy_args[@]}"
  exit 0
fi

printf 'Deploying. First run provisions PostgreSQL and takes several minutes.\n\n'
az deployment group create "${deploy_args[@]}" --name "haechi-platform-$(date +%Y%m%d%H%M%S)" --output none

outputs="$(az deployment group show \
  --resource-group "${resource_group}" \
  --name "$(az deployment group list --resource-group "${resource_group}" --query '[0].name' -o tsv)" \
  --query properties.outputs -o json)"

storage_account="$(printf '%s' "${outputs}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["storageAccountName"]["value"])')"
policy_share="$(printf '%s' "${outputs}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["policyShareName"]["value"])')"

# OPA loads its policy from the mounted share, so the share must hold the
# policy before OPA can answer anything. Uploading here keeps the deployed
# policy identical to the one CI tests in deploy/opa/policy.
printf '\nUploading OPA policy from deploy/opa/policy...\n'
storage_key="$(az storage account keys list \
  --account-name "${storage_account}" \
  --resource-group "${resource_group}" \
  --query '[0].value' -o tsv)"

for policy in "${REPO_ROOT}"/deploy/opa/policy/*.rego; do
  [[ -e "${policy}" ]] || fail "no .rego policy found in deploy/opa/policy"
  # Test files define no policy OPA should serve. Uploading them would put
  # test fixtures on the decision path.
  [[ "${policy}" == *_test.rego ]] && continue
  az storage file upload \
    --account-name "${storage_account}" \
    --account-key "${storage_key}" \
    --share-name "${policy_share}" \
    --source "${policy}" \
    --path "$(basename "${policy}")" \
    --only-show-errors \
    --output none
  printf '  uploaded %s\n' "$(basename "${policy}")"
done

printf '\nDeployment complete.\n\n'
printf '%s\n' "${outputs}" | python3 -c '
import json, sys
for key, item in json.load(sys.stdin).items():
    print("  {0:24} {1}".format(key, item["value"]))
'

cat <<"NEXT"

Next steps, in order:

  1. Apply the database schemas. Both are required and they are separate
     databases by design (ADR-004):

       psql "$CONTROL_URL" -f deploy/postgres/schema.sql
       psql "$REPLAY_URL"  -f deploy/postgres/replay-schema.sql

  2. Create the Keycloak realm "haechi" and a client with audience
     "haechi-control-api". The control API refuses to start outside
     development without them.

  3. Enable Transit signing in OpenBao and create the key:

       bao secrets enable transit
       bao write -f transit/keys/haechi-control type=ed25519

  4. Build and push the control API image, then redeploy with
     HAECHI_CONTROL_API_IMAGE set to it.

NEXT
