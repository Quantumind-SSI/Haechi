// Haechi platform environment.
//
// This template provisions the control-plane platform: PostgreSQL, Keycloak,
// OpenBao, OPA and the control API. It is deliberately separate from
// main.bicep, which provisions the marketing website and shares nothing with
// this stack beyond a subscription.
//
// Scope is bounded by what the code actually consumes. cmd/control-api reads
// HAECHI_DATABASE_URL, KEYCLOAK_ISSUER, KEYCLOAK_AUDIENCE, OPA_URL,
// OPENBAO_ADDR and OPENBAO_TOKEN. Nothing imports adapters/nats, so no message
// broker is provisioned here; adding one would be infrastructure for code that
// does not exist. The same applies to ClickHouse, Rosenpass and Xray-core,
// which are phase 3 to 5 in third_party/manifest.yaml.
//
// Network posture: public endpoints restricted by IP allowlist, chosen for a
// single-developer environment. PostgreSQL is reachable only from the listed
// addresses and from Azure services. Only Keycloak and the control API accept
// external ingress, both IP restricted. OPA and OpenBao are internal to the
// Container Apps environment and are not routable from the internet.

targetScope = 'resourceGroup'

@description('Environment name, used as a resource name component. Lower case.')
@minLength(3)
@maxLength(12)
param environmentName string

@description('Azure region for compute.')
param location string = resourceGroup().location

@description('''
Azure region for PostgreSQL. Defaults to the compute region.

Staging separates the two because this subscription permits one Container Apps
managed environment per region and the website production stack already holds
UK South, while PostgreSQL Flexible Server is restricted from provisioning in
UK West on this subscription. Compute therefore runs in UK West and the
database in UK South. Both are in the United Kingdom, and the round trip
between them is a few milliseconds, which a staging control plane absorbs.
''')
param databaseLocation string = location

@description('IPv4 addresses permitted to reach PostgreSQL and the external ingresses. Supply at least the operator address.')
@minLength(1)
param allowedIpAddresses array

@description('PostgreSQL administrator login.')
@minLength(4)
param databaseAdministratorLogin string

@description('PostgreSQL administrator password.')
@secure()
@minLength(16)
param databaseAdministratorPassword string

@description('Keycloak initial administrator password.')
@secure()
@minLength(16)
param keycloakAdminPassword string

@description('OpenBao development root token. See the runbook: this environment runs OpenBao in development mode and its Transit key does not survive a restart of that container.')
@secure()
@minLength(16)
param openBaoRootToken string

@description('Control API image. The seed only exists so the first deployment can create the app before any image is pushed; the deploy workflow replaces it.')
param controlApiImage string = 'mcr.microsoft.com/k8se/quickstart:latest'

@description('PostgreSQL SKU.')
param databaseSkuName string = 'Standard_B1ms'

@description('PostgreSQL tier.')
@allowed(['Burstable', 'GeneralPurpose', 'MemoryOptimized'])
param databaseTier string = 'Burstable'

@description('PostgreSQL backup retention in days.')
@minValue(7)
@maxValue(35)
param backupRetentionDays int = 7

@description('Minimum replicas for the always-on platform services. Set to 0 to trade cold starts for cost.')
@minValue(0)
@maxValue(5)
param platformMinReplicas int = 1

@description('Minimum replicas for the control API. Zero is reasonable for a single developer.')
@minValue(0)
@maxValue(5)
param controlApiMinReplicas int = 0

var suffix = uniqueString(resourceGroup().id, environmentName)
var namePrefix = 'haechi-${environmentName}'
var registryName = 'haechiplat${environmentName}${suffix}'
var storageName = take('haechiplat${environmentName}${suffix}', 24)
var acrPullRoleId = '7f951dda-4ed3-4680-a7ca-43fe172d538d'

// Three databases on one server. The control and replay schemas are separate
// databases, not separate schemas, because a gateway must never hold
// credentials to identity-linked control-plane state (ADR-004,
// deploy/postgres/replay-schema.sql). A single server is a staging
// concession; production must separate them further, by server or by role.
var controlDatabaseName = 'haechi_control'
var replayDatabaseName = 'haechi_replay'
var keycloakDatabaseName = 'keycloak'

// Container Apps ingress rules take a CIDR range, not a bare address. A single
// address is expressed as a /32. Getting this wrong silently admits everyone,
// because an ingress with no valid rules is unrestricted.
var ipRules = [
  for (ip, i) in allowedIpAddresses: {
    name: 'operator-${i}'
    ipAddressRange: '${ip}/32'
    action: 'Allow'
    description: 'Operator address ${i}'
  }
]

// ---------------------------------------------------------------------------
// Observability
// ---------------------------------------------------------------------------

resource logs 'Microsoft.OperationalInsights/workspaces@2023-09-01' = {
  name: '${namePrefix}-plat-logs'
  location: location
  properties: {
    sku: { name: 'PerGB2018' }
    retentionInDays: 30
  }
}

// ---------------------------------------------------------------------------
// Registry and identity
// ---------------------------------------------------------------------------

resource identity 'Microsoft.ManagedIdentity/userAssignedIdentities@2023-01-31' = {
  name: '${namePrefix}-plat-identity'
  location: location
}

resource registry 'Microsoft.ContainerRegistry/registries@2023-07-01' = {
  name: registryName
  location: location
  sku: { name: 'Basic' }
  properties: {
    adminUserEnabled: false
  }
}

resource acrPull 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(registry.id, identity.id, acrPullRoleId)
  scope: registry
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', acrPullRoleId)
    principalId: identity.properties.principalId
    principalType: 'ServicePrincipal'
  }
}

// ---------------------------------------------------------------------------
// PostgreSQL
// ---------------------------------------------------------------------------

resource postgres 'Microsoft.DBforPostgreSQL/flexibleServers@2024-08-01' = {
  name: '${namePrefix}-plat-postgres'
  location: databaseLocation
  sku: {
    name: databaseSkuName
    tier: databaseTier
  }
  properties: {
    version: '16'
    administratorLogin: databaseAdministratorLogin
    administratorLoginPassword: databaseAdministratorPassword
    storage: {
      storageSizeGB: 32
      autoGrow: 'Enabled'
    }
    backup: {
      backupRetentionDays: backupRetentionDays
      geoRedundantBackup: 'Disabled'
    }
    highAvailability: { mode: 'Disabled' }
    network: {
      publicNetworkAccess: 'Enabled'
    }
  }
}

// TLS is not optional. Without this a misconfigured client can negotiate a
// plaintext session to a database reachable from the public internet.
resource requireTls 'Microsoft.DBforPostgreSQL/flexibleServers/configurations@2024-08-01' = {
  parent: postgres
  name: 'require_secure_transport'
  properties: {
    value: 'ON'
    source: 'user-override'
  }
}

// Container Apps consumption workloads egress from shared Azure address space,
// so they are admitted by the Azure services rule rather than by static IP.
resource allowAzure 'Microsoft.DBforPostgreSQL/flexibleServers/firewallRules@2024-08-01' = {
  parent: postgres
  name: 'allow-azure-services'
  properties: {
    startIpAddress: '0.0.0.0'
    endIpAddress: '0.0.0.0'
  }
  dependsOn: [requireTls]
}

resource allowOperators 'Microsoft.DBforPostgreSQL/flexibleServers/firewallRules@2024-08-01' = [
  for (ip, i) in allowedIpAddresses: {
    parent: postgres
    name: 'operator-${i}'
    properties: {
      startIpAddress: ip
      endIpAddress: ip
    }
    dependsOn: [allowAzure]
  }
]

resource controlDatabase 'Microsoft.DBforPostgreSQL/flexibleServers/databases@2024-08-01' = {
  parent: postgres
  name: controlDatabaseName
  properties: {
    charset: 'UTF8'
    collation: 'en_US.utf8'
  }
}

resource replayDatabase 'Microsoft.DBforPostgreSQL/flexibleServers/databases@2024-08-01' = {
  parent: postgres
  name: replayDatabaseName
  properties: {
    charset: 'UTF8'
    collation: 'en_US.utf8'
  }
  dependsOn: [controlDatabase]
}

resource keycloakDatabase 'Microsoft.DBforPostgreSQL/flexibleServers/databases@2024-08-01' = {
  parent: postgres
  name: keycloakDatabaseName
  properties: {
    charset: 'UTF8'
    collation: 'en_US.utf8'
  }
  dependsOn: [replayDatabase]
}

// ---------------------------------------------------------------------------
// Storage for the OPA policy bundle
// ---------------------------------------------------------------------------

resource storage 'Microsoft.Storage/storageAccounts@2023-05-01' = {
  name: storageName
  location: location
  sku: { name: 'Standard_LRS' }
  kind: 'StorageV2'
  properties: {
    minimumTlsVersion: 'TLS1_2'
    allowBlobPublicAccess: false
    supportsHttpsTrafficOnly: true
  }
}

resource fileServices 'Microsoft.Storage/storageAccounts/fileServices@2023-05-01' = {
  parent: storage
  name: 'default'
}

resource policyShare 'Microsoft.Storage/storageAccounts/fileServices/shares@2023-05-01' = {
  parent: fileServices
  name: 'opa-policy'
  properties: {
    shareQuota: 1
  }
}

// ---------------------------------------------------------------------------
// Container Apps environment
// ---------------------------------------------------------------------------

resource environment 'Microsoft.App/managedEnvironments@2024-03-01' = {
  name: '${namePrefix}-plat-env'
  location: location
  properties: {
    appLogsConfiguration: {
      destination: 'log-analytics'
      logAnalyticsConfiguration: {
        customerId: logs.properties.customerId
        sharedKey: logs.listKeys().primarySharedKey
      }
    }
  }
}

resource policyStorage 'Microsoft.App/managedEnvironments/storages@2024-03-01' = {
  parent: environment
  name: 'opa-policy'
  properties: {
    azureFile: {
      accountName: storage.name
      accountKey: storage.listKeys().keys[0].value
      shareName: policyShare.name
      accessMode: 'ReadOnly'
    }
  }
}

// ---------------------------------------------------------------------------
// Keycloak. Identity federation. Mandatory: cmd/control-api refuses to start
// outside development without KEYCLOAK_ISSUER and KEYCLOAK_AUDIENCE.
// ---------------------------------------------------------------------------

resource keycloak 'Microsoft.App/containerApps@2024-03-01' = {
  name: '${namePrefix}-keycloak'
  location: location
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: { '${identity.id}': {} }
  }
  properties: {
    managedEnvironmentId: environment.id
    configuration: {
      ingress: {
        external: true
        targetPort: 8080
        transport: 'auto'
        allowInsecure: false
        ipSecurityRestrictions: ipRules
      }
      secrets: [
        {
          name: 'admin-password'
          value: keycloakAdminPassword
        }
        {
          name: 'db-password'
          value: databaseAdministratorPassword
        }
      ]
    }
    template: {
      containers: [
        {
          name: 'keycloak'
          image: 'quay.io/keycloak/keycloak:25.0.6'
          // start-dev keeps the developer-friendly console while persisting to
          // PostgreSQL, so realms and clients survive a restart. Production
          // requires `start` with a configured hostname and TLS chain.
          args: ['start-dev', '--http-enabled=true', '--proxy-headers=xforwarded']
          resources: {
            cpu: json('0.5')
            memory: '1.0Gi'
          }
          env: [
            { name: 'KC_DB', value: 'postgres' }
            { name: 'KC_DB_URL', value: 'jdbc:postgresql://${postgres.properties.fullyQualifiedDomainName}:5432/${keycloakDatabaseName}?sslmode=require' }
            { name: 'KC_DB_USERNAME', value: databaseAdministratorLogin }
            { name: 'KC_DB_PASSWORD', secretRef: 'db-password' }
            { name: 'KC_BOOTSTRAP_ADMIN_USERNAME', value: 'admin' }
            { name: 'KC_BOOTSTRAP_ADMIN_PASSWORD', secretRef: 'admin-password' }
            { name: 'KC_HEALTH_ENABLED', value: 'true' }
          ]
        }
      ]
      scale: {
        minReplicas: platformMinReplicas
        maxReplicas: 1
      }
    }
  }
  dependsOn: [keycloakDatabase, acrPull]
}

// ---------------------------------------------------------------------------
// OPA. Authorisation. Internal only: nothing outside the environment should be
// able to ask it for a decision.
// ---------------------------------------------------------------------------

resource opa 'Microsoft.App/containerApps@2024-03-01' = {
  name: '${namePrefix}-opa'
  location: location
  properties: {
    managedEnvironmentId: environment.id
    configuration: {
      ingress: {
        external: false
        targetPort: 8181
        transport: 'auto'
        allowInsecure: true
      }
    }
    template: {
      containers: [
        {
          name: 'opa'
          image: 'openpolicyagent/opa:0.70.0'
          args: ['run', '--server', '--addr=0.0.0.0:8181', '/policy']
          resources: {
            cpu: json('0.25')
            memory: '0.5Gi'
          }
          volumeMounts: [
            {
              volumeName: 'policy'
              mountPath: '/policy'
            }
          ]
        }
      ]
      volumes: [
        {
          name: 'policy'
          storageType: 'AzureFile'
          storageName: policyStorage.name
        }
      ]
      scale: {
        minReplicas: platformMinReplicas
        maxReplicas: 1
      }
    }
  }
}

// ---------------------------------------------------------------------------
// OpenBao. Transit signing.
//
// DEVELOPMENT MODE. Storage is in memory, so the Transit key is destroyed when
// this container restarts and previously signed material stops verifying. This
// matches deploy/compose/docker-compose.yaml and is acceptable for staging,
// where the alternative in cmd/control-api is a key regenerated on every
// control-api restart. Production requires file or raft storage with
// auto-unseal, which is release blocker 8.
// ---------------------------------------------------------------------------

resource openbao 'Microsoft.App/containerApps@2024-03-01' = {
  name: '${namePrefix}-openbao'
  location: location
  properties: {
    managedEnvironmentId: environment.id
    configuration: {
      ingress: {
        external: false
        targetPort: 8200
        transport: 'auto'
        allowInsecure: true
      }
      secrets: [
        {
          name: 'root-token'
          value: openBaoRootToken
        }
      ]
    }
    template: {
      containers: [
        {
          name: 'openbao'
          image: 'quay.io/openbao/openbao:2.0.1'
          resources: {
            cpu: json('0.25')
            memory: '0.5Gi'
          }
          env: [
            { name: 'BAO_DEV_ROOT_TOKEN_ID', secretRef: 'root-token' }
            { name: 'BAO_DEV_LISTEN_ADDRESS', value: '0.0.0.0:8200' }
          ]
        }
      ]
      scale: {
        // Never zero. Scaling to zero in development mode destroys the Transit
        // key, which is worse than the cost of one small always-on replica.
        minReplicas: 1
        maxReplicas: 1
      }
    }
  }
}

// ---------------------------------------------------------------------------
// Control API
// ---------------------------------------------------------------------------

resource controlApi 'Microsoft.App/containerApps@2024-03-01' = {
  name: '${namePrefix}-control-api'
  location: location
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: { '${identity.id}': {} }
  }
  properties: {
    managedEnvironmentId: environment.id
    configuration: {
      ingress: {
        external: true
        targetPort: 8080
        transport: 'auto'
        allowInsecure: false
        ipSecurityRestrictions: ipRules
      }
      registries: [
        {
          server: registry.properties.loginServer
          identity: identity.id
        }
      ]
      secrets: [
        {
          name: 'control-database-url'
          value: 'postgresql://${databaseAdministratorLogin}:${uriComponent(databaseAdministratorPassword)}@${postgres.properties.fullyQualifiedDomainName}:5432/${controlDatabaseName}?sslmode=verify-full'
        }
        {
          name: 'replay-database-url'
          value: 'postgresql://${databaseAdministratorLogin}:${uriComponent(databaseAdministratorPassword)}@${postgres.properties.fullyQualifiedDomainName}:5432/${replayDatabaseName}?sslmode=verify-full'
        }
        {
          name: 'openbao-token'
          value: openBaoRootToken
        }
      ]
    }
    template: {
      containers: [
        {
          name: 'control-api'
          image: controlApiImage
          resources: {
            cpu: json('0.5')
            memory: '1.0Gi'
          }
          env: [
            { name: 'HAECHI_ENV', value: environmentName }
            { name: 'HAECHI_LISTEN', value: ':8080' }
            { name: 'HAECHI_DATABASE_URL', secretRef: 'control-database-url' }
            { name: 'HAECHI_REPLAY_DATABASE_URL', secretRef: 'replay-database-url' }
            { name: 'OPA_URL', value: 'http://${opa.properties.configuration.ingress.fqdn}' }
            { name: 'OPENBAO_ADDR', value: 'http://${openbao.properties.configuration.ingress.fqdn}' }
            { name: 'OPENBAO_TOKEN', secretRef: 'openbao-token' }
            { name: 'OPENBAO_KEY', value: 'haechi-control' }
            { name: 'KEYCLOAK_ISSUER', value: 'https://${keycloak.properties.configuration.ingress.fqdn}/realms/haechi' }
            { name: 'KEYCLOAK_AUDIENCE', value: 'haechi-control-api' }
          ]
        }
      ]
      scale: {
        minReplicas: controlApiMinReplicas
        maxReplicas: 2
      }
    }
  }
}

output registryLoginServer string = registry.properties.loginServer
output registryName string = registry.name
output postgresFqdn string = postgres.properties.fullyQualifiedDomainName
output controlDatabaseName string = controlDatabaseName
output replayDatabaseName string = replayDatabaseName
output keycloakUrl string = 'https://${keycloak.properties.configuration.ingress.fqdn}'
output controlApiUrl string = 'https://${controlApi.properties.configuration.ingress.fqdn}'
output storageAccountName string = storage.name
output policyShareName string = policyShare.name
output identityId string = identity.id
