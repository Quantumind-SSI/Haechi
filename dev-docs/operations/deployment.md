# Website Deployment Runbook

Covers the public website only. The Haechi control plane and agent are not
deployable; see [`../engineering/readiness.md`](../engineering/readiness.md).

## Topology

```
Browser
  |
  |  static assets (HTML, CSS, JS, config.js, _headers)
  +--> Cloudflare Pages          project: haechi-website
  |
  |  POST /api/pilot  (cross-origin, CORS allowlisted)
  +--> Azure Container Apps      rg-haechi-<env>-uksouth
           |
           |  private VNet, no public endpoint
           +--> Azure Database for PostgreSQL Flexible Server
```

Cloudflare stores nothing. Azure owns the API and all submitted data, in UK
South. The two sides are coupled by exactly one value: the API base URL, which
is baked into the Cloudflare build.

## Prerequisites

| Requirement                          | Detail                                               |
| ------------------------------------ | ---------------------------------------------------- |
| Azure subscription                   | Contributor on the target subscription               |
| Entra app with federated credentials | For GitHub OIDC, no client secret                    |
| Cloudflare account                   | API token with Pages edit permission                 |
| Local tooling                        | Azure CLI with the Bicep extension, Node 20 or newer |

### Repository and environment secrets

| Secret                   | Used by             | Purpose                            |
| ------------------------ | ------------------- | ---------------------------------- |
| `AZURE_CLIENT_ID`        | Azure workflow      | Federated identity                 |
| `AZURE_TENANT_ID`        | Azure workflow      | Federated identity                 |
| `AZURE_SUBSCRIPTION_ID`  | Azure workflow      | Target subscription                |
| `HAECHI_DB_PASSWORD`     | Azure workflow      | PostgreSQL administrator password  |
| `HAECHI_PILOT_IP_SALT`   | Azure workflow      | HMAC key, minimum 32 characters    |
| `HAECHI_ALLOWED_ORIGINS` | Azure workflow      | Origins permitted to post the form |
| `CLOUDFLARE_API_TOKEN`   | Cloudflare workflow | Pages deployment                   |
| `CLOUDFLARE_ACCOUNT_ID`  | Cloudflare workflow | Target account                     |
| `HAECHI_API_BASE`        | Cloudflare workflow | Azure API origin, https            |

Generate the salt with `openssl rand -hex 32`. Changing it later invalidates
every stored pseudonym, which resets rate limiting but destroys no submissions.

## First deployment

Order matters. Azure must exist before Cloudflare, because the Cloudflare build
needs the API origin.

### 1. Provision Azure

```bash
export AZURE_SUBSCRIPTION_ID="..."
export HAECHI_DB_PASSWORD="..."
export HAECHI_PILOT_IP_SALT="$(openssl rand -hex 32)"
export HAECHI_ALLOWED_ORIGINS="https://haechi.example"
export HAECHI_ENVIRONMENT="staging"

deploy/azure/deploy.sh
```

The script validates the template before deploying, then prints the outputs.
Record `apiBaseUrl`. The first run uses a placeholder image so the container
app can be created; the workflow replaces it on the next step.

### 2. Deploy the application

Run the **Deploy Website API to Azure** workflow for the same environment. It
builds the image, pushes it to the registry, applies migrations, releases the
revision, and fails the run if the health endpoint does not return 200.

Migrations run as a Container Apps job inside the virtual network. They cannot
run from a CI runner, because the database has no public endpoint.

### 3. Deploy Cloudflare

Set `HAECHI_API_BASE` to the `apiBaseUrl` from step 1, then run the **Deploy
Website to Cloudflare Pages** workflow. It fails if the API origin is missing
from either `config.js` or `_headers`.

### 4. Verify

```bash
curl -sS "$HAECHI_API_BASE/api/health"          # {"status":"ok",...}
curl -sI https://haechi.example | grep -i content-security-policy
```

Submit the form once from the live site and confirm one row lands in the
database. Confirm a second browser origin is rejected with 403.

## Routine deployment

1. Merge to `main` and let CI pass.
2. Run the Azure workflow. It is safe to re-run: Bicep converges and migrations
   are idempotent.
3. Run the Cloudflare workflow only when static assets or the API origin change.

## Rollback

The container app keeps previous revisions.

```bash
az containerapp revision list -n haechi-prod-website -g rg-haechi-prod-uksouth -o table
az containerapp ingress traffic set -n haechi-prod-website -g rg-haechi-prod-uksouth \
  --revision-weight <previous-revision>=100
```

Migrations are additive and idempotent, so an application rollback does not
require a database rollback. Any future migration that drops or rewrites a
column must ship with a documented reverse step before it is merged.

Cloudflare Pages keeps prior deployments; roll back from the Pages dashboard or
by re-running the workflow from an earlier commit.

## Operational limits

| Property              | Value                                                    |
| --------------------- | -------------------------------------------------------- |
| Region                | UK South                                                 |
| Replicas              | 1 minimum, 3 maximum, scaling at 100 concurrent requests |
| Database              | Burstable B1ms, 32 GB, auto-grow enabled                 |
| Backups               | 14 days, not geo-redundant                               |
| Log retention         | 30 days in Log Analytics                                 |
| Submission rate limit | 5 per hour per pseudonymised address                     |

## Known gaps

These are unresolved and must be closed before the site handles real customer
data at volume.

1. No approved retention or deletion schedule, so the privacy notice states
   that no automatic deletion runs.
2. The database password is a deployment parameter held in GitHub secrets.
   Moving to Key Vault references or Entra managed identity authentication
   removes the stored password entirely.
3. No geo-redundant backup and no high availability. Acceptable for a marketing
   site, not for anything carrying a recovery objective.
4. No alerting on failed submissions, health probe failures, or database
   saturation. Log Analytics collects the data but nothing pages a human.
5. The deployment workflows have never executed. Every step is syntax checked
   and the template compiles, but no run has been observed end to end.
