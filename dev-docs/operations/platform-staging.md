# Platform Staging Runbook

Status: Authoritative
Provisioned: 2026-08-31

The control-plane platform environment: PostgreSQL, Keycloak, OpenBao, OPA and
the control API. This is a separate stack from the marketing website, which is
covered by [`deployment.md`](./deployment.md). They share a subscription and
nothing else.

## Topology

```
Subscription: Azure subscription 1 (eec4af79-...)

  rg-haechi-staging-platform-ukwest
    +--> Container Apps environment       haechi-staging-plat-env      (UK West)
    |      +--> haechi-staging-keycloak     external, IP restricted
    |      +--> haechi-staging-control-api  external, IP restricted
    |      +--> haechi-staging-opa          internal only
    |      +--> haechi-staging-openbao      internal only
    +--> PostgreSQL Flexible Server       haechi-staging-plat-postgres (UK South)
    |      +--> haechi_control
    |      +--> haechi_replay
    |      +--> keycloak
    +--> Container Registry, Storage, Log Analytics, Managed Identity
```

### Why compute and database are in different regions

Two subscription constraints collide. Azure permits one Container Apps managed
environment per region on this subscription, and the website production stack
already holds UK South. PostgreSQL Flexible Server is separately restricted
from provisioning in UK West on this subscription:

```
$ az postgres flexible-server list-skus --location ukwest --query "[0].reason"
"Subscriptions are restricted from provisioning in this region."
```

Compute therefore runs in UK West and the database in UK South. Both are in the
United Kingdom, so the residency position is unchanged, and the round trip
between the two is a few milliseconds. If the Container Apps quota is raised,
collapse both into UK South by removing `databaseLocation` from the parameter
file.

## What is deliberately absent

**NATS.** Nothing imports `adapters/nats` and `cmd/control-api` reads no NATS
variable. Provisioning a broker no code connects to would be infrastructure for
software that does not exist.

**ClickHouse, Rosenpass, Xray-core, ONNX Runtime.** Phases 3 to 5 in
`third_party/manifest.yaml`. None are referenced by any executable.

**AKS, Helm, OpenTofu.** The `deploy/kubernetes`, `deploy/helm` and
`deploy/opentofu` directories are empty and stay that way until the WireGuard
data plane is real. Container Apps cannot run it: it needs `NET_ADMIN`, kernel
modules and host networking. That is release blocker 3.

## Known limitations

**OpenBao runs in development mode.** Storage is in memory, so the Transit key
is destroyed if that container restarts, and anything signed with the previous
key stops verifying. This matches `deploy/compose/docker-compose.yaml`. It is
still an improvement on the alternative, because with `OPENBAO_ADDR` unset
`cmd/control-api` generates a fresh in-process key on every one of its own
restarts. Its replica count is pinned at 1 and must not be set to zero.
Production requires file or raft storage with auto-unseal, which is release
blocker 8.

**The database administrator account is shared.** All three databases use one
administrator login. The trust separation in
[`../../deploy/postgres/replay-schema.sql`](../../deploy/postgres/replay-schema.sql)
requires the gateway to reach only `haechi_replay`. Staging does not yet model
that. Before a gateway runs anywhere real, create a dedicated role:

```sql
CREATE ROLE haechi_gateway LOGIN PASSWORD '...';
GRANT CONNECT ON DATABASE haechi_replay TO haechi_gateway;
GRANT SELECT, INSERT, DELETE ON capability_replay TO haechi_gateway;
```

**No migration runner.** Schemas are applied by hand with `psql`. A versioned
runner is required before the first destructive schema change, and is an open
item under release blocker 1.

## Prerequisites

| Requirement        | Detail                                               |
| ------------------ | ---------------------------------------------------- |
| Azure subscription | Contributor on the target subscription               |
| Local tooling      | Azure CLI with the Bicep extension, `psql`, Python 3 |
| Container runtime  | Only to build and push the control API image         |

## Provisioning

Idempotent. Re-running converges the resource group.

```bash
export AZURE_SUBSCRIPTION_ID=...
export HAECHI_PLAT_DB_PASSWORD=...   # 16+ characters
export HAECHI_PLAT_KC_PASSWORD=...   # 16+ characters
export HAECHI_PLAT_BAO_TOKEN=...     # 16+ characters
export HAECHI_LOCATION=ukwest

# Preview without creating anything:
HAECHI_WHATIF=1 ./deploy/azure/deploy-platform.sh

./deploy/azure/deploy-platform.sh
```

The allowlist defaults to the address this machine presents to Azure. Override
with `HAECHI_ALLOWED_IPS=1.2.3.4,5.6.7.8`. An empty allowlist is refused rather
than silently opening the ingress. **A changing home IP address will lock you
out**; re-run the script to refresh the rules.

## Post-provision steps

### 1. Apply the schemas

Both are required, and they are separate databases by design.

```bash
PGFQDN=$(az postgres flexible-server show -n haechi-staging-plat-postgres \
  -g rg-haechi-staging-platform-ukwest --query fullyQualifiedDomainName -o tsv)
BASE="host=$PGFQDN port=5432 user=haechiadmin sslmode=verify-full sslrootcert=system"

psql "$BASE dbname=haechi_control" -v ON_ERROR_STOP=1 -f deploy/postgres/schema.sql
psql "$BASE dbname=haechi_replay"  -v ON_ERROR_STOP=1 -f deploy/postgres/replay-schema.sql
```

`sslrootcert=system` is needed because `verify-full` otherwise looks for
`~/.postgresql/root.crt`. Go's pgx driver uses the system roots already, so the
deployed application needs no equivalent setting.

### 2. Create the Keycloak realm

`cmd/control-api` refuses to start outside development without
`KEYCLOAK_ISSUER` and `KEYCLOAK_AUDIENCE`. The template points the issuer at a
realm named `haechi` and an audience of `haechi-control-api`, so both must
exist. Sign in to the Keycloak console with `admin` and the bootstrap password,
create the realm, then create a client with that audience.

### 3. Enable Transit signing

```bash
bao secrets enable transit
bao write -f transit/keys/haechi-control type=ed25519
```

### 4. Push the control API image

```bash
REGISTRY=$(az deployment group show -g rg-haechi-staging-platform-ukwest \
  -n <deployment> --query properties.outputs.registryLoginServer.value -o tsv)
az acr login --name "${REGISTRY%%.*}"
docker build -f deploy/docker/control-api.Dockerfile -t "$REGISTRY/haechi-control-api:$(git rev-parse --short HEAD)" .
docker push "$REGISTRY/haechi-control-api:$(git rev-parse --short HEAD)"

HAECHI_CONTROL_API_IMAGE="$REGISTRY/haechi-control-api:$(git rev-parse --short HEAD)" \
  ./deploy/azure/deploy-platform.sh
```

## Running the integration suites against staging

The replay suite runs against the deployed database, which is how the shared
store was verified end to end:

```bash
PW=$(python3 -c "import urllib.parse;print(urllib.parse.quote(input(),safe=''))" <<<"$HAECHI_PLAT_DB_PASSWORD")
export HAECHI_REPLAY_TEST_DATABASE_URL="postgresql://haechiadmin:${PW}@${PGFQDN}:5432/haechi_replay?sslmode=verify-full"
go test ./adapters/postgres/ -count=1 -run Replay -v
```

These tests write and delete rows. Do not point them at production.

## Cost

Roughly £55 to £75 per month at the committed settings. The dominant cost is
three always-on replicas: Keycloak at 0.5 vCPU, OPA and OpenBao at 0.25 each.

To reduce it:

- `platformMinReplicas: 0` saves most of the compute cost, at the price of cold
  starts. Do not apply it to OpenBao while it runs in development mode, because
  scaling to zero destroys the Transit key.
- The control API already defaults to `controlApiMinReplicas: 0`.
- `az group delete --name rg-haechi-staging-platform-ukwest` removes the whole
  environment. It is fully reproducible from the template.

## Teardown

```bash
az group delete --name rg-haechi-staging-platform-ukwest --yes
```

Nothing in this environment is a source of truth. Schemas are in
`deploy/postgres`, policy in `deploy/opa/policy`, and everything else in
`deploy/azure/platform.bicep`.
