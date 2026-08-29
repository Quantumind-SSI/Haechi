#!/usr/bin/env bash
#
# Provisions or updates the Haechi website infrastructure in Azure.
#
# This script is idempotent: Bicep deployments are declarative, so re-running
# converges the resource group to the template rather than duplicating it.
#
# Required environment:
#   AZURE_SUBSCRIPTION_ID   target subscription
#   HAECHI_DB_PASSWORD      PostgreSQL administrator password
#   HAECHI_PILOT_IP_SALT    HMAC key, at least 32 characters
#   HAECHI_ALLOWED_ORIGINS  comma separated origins allowed to post the form
#
# Optional environment:
#   HAECHI_ENVIRONMENT      prod (default) or staging
#   HAECHI_LOCATION         uksouth (default)
#   HAECHI_IMAGE            image to run; defaults to the seed image below
#
# The seed image only exists so the first deployment can create the container
# app before any application image has been pushed. The deployment workflow
# replaces it immediately afterwards.

set -euo pipefail

readonly SEED_IMAGE="mcr.microsoft.com/k8se/quickstart:latest"
readonly MINIMUM_SALT_LENGTH=32

fail() {
  printf 'error: %s\n' "$1" >&2
  exit 1
}

require_var() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    fail "$name is required"
  fi
}

require_var AZURE_SUBSCRIPTION_ID
require_var HAECHI_DB_PASSWORD
require_var HAECHI_PILOT_IP_SALT
require_var HAECHI_ALLOWED_ORIGINS

if (( ${#HAECHI_PILOT_IP_SALT} < MINIMUM_SALT_LENGTH )); then
  fail "HAECHI_PILOT_IP_SALT must be at least ${MINIMUM_SALT_LENGTH} characters"
fi

environment="${HAECHI_ENVIRONMENT:-prod}"
location="${HAECHI_LOCATION:-uksouth}"
image="${HAECHI_IMAGE:-$SEED_IMAGE}"
resource_group="rg-haechi-${environment}-${location}"
template="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/main.bicep"

command -v az >/dev/null 2>&1 || fail "the Azure CLI is not installed"
[[ -f "$template" ]] || fail "template not found: $template"

printf 'subscription:   %s\n' "$AZURE_SUBSCRIPTION_ID"
printf 'resource group: %s\n' "$resource_group"
printf 'location:       %s\n' "$location"
printf 'image:          %s\n\n' "$image"

az account set --subscription "$AZURE_SUBSCRIPTION_ID"

az group create \
  --name "$resource_group" \
  --location "$location" \
  --output none

# Validate before deploying so template errors surface without partial changes.
az deployment group validate \
  --resource-group "$resource_group" \
  --template-file "$template" \
  --parameters \
    environmentName="$environment" \
    location="$location" \
    containerImage="$image" \
    databaseAdministratorPassword="$HAECHI_DB_PASSWORD" \
    pilotIpSalt="$HAECHI_PILOT_IP_SALT" \
    allowedOrigins="$HAECHI_ALLOWED_ORIGINS" \
  --output none

az deployment group create \
  --name "haechi-$(date -u +%Y%m%d%H%M%S)" \
  --resource-group "$resource_group" \
  --template-file "$template" \
  --parameters \
    environmentName="$environment" \
    location="$location" \
    containerImage="$image" \
    databaseAdministratorPassword="$HAECHI_DB_PASSWORD" \
    pilotIpSalt="$HAECHI_PILOT_IP_SALT" \
    allowedOrigins="$HAECHI_ALLOWED_ORIGINS" \
  --query properties.outputs \
  --output json
