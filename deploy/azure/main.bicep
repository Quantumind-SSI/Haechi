// Haechi website origin on Azure Container Apps, UK South.
//
// Topology:
//   Cloudflare Pages serves static assets at the edge.
//   This deployment owns the pilot API and its data.
//
//   Container App  --(private VNet)-->  PostgreSQL Flexible Server
//         |                                   private DNS only
//         +-- pulls image from ACR via user-assigned managed identity
//
// PostgreSQL has no public endpoint. It is reachable only from the delegated
// subnet, so the database cannot be dialled from the internet even with valid
// credentials.
//
// Deploy with deploy/azure/deploy.sh, which is the supported entry point.

targetScope = 'resourceGroup'

@description('Environment discriminator used in resource names.')
@allowed(['prod', 'staging'])
param environmentName string = 'prod'

@description('Azure region. UK South keeps personal data in the United Kingdom.')
param location string = 'uksouth'

@description('Container image reference, including registry, repository and tag.')
param containerImage string

@description('Administrator login for PostgreSQL.')
param databaseAdministratorLogin string = 'haechi_admin'

@description('Administrator password for PostgreSQL. Supply from a secret store.')
@secure()
param databaseAdministratorPassword string

@description('HMAC key used to pseudonymise client addresses. At least 32 characters.')
@secure()
param pilotIpSalt string

@description('Origins permitted to post the pilot form, comma separated.')
param allowedOrigins string

@description('Minimum number of running replicas. One keeps the API warm.')
@minValue(0)
@maxValue(5)
param minReplicas int = environmentName == 'prod' ? 2 : 1

@description('Maximum number of replicas.')
@minValue(1)
@maxValue(10)
param maxReplicas int = 3

// --- Durability -------------------------------------------------------------
// Staging exists to be cheap and disposable. Production holds enterprise
// contact details that cannot be re-collected if they are lost, so it pays for
// geo-redundant backup and a longer recovery window.

@description('Backup retention in days. Production keeps a longer window.')
@minValue(7)
@maxValue(35)
param backupRetentionDays int = environmentName == 'prod' ? 35 : 14

@description('Geo-redundant backup. Protects the data against loss of the region.')
@allowed(['Enabled', 'Disabled'])
param geoRedundantBackup string = environmentName == 'prod' ? 'Enabled' : 'Disabled'

// Azure rejects high availability on the Burstable tier at deployment time with
// HANotSupportedForBurstableSkuWithMoreInfo. Note that neither the capabilities
// API nor `az deployment group validate` reports this: Standard_B1ms advertises
// supportedHaMode [SameZone, ZoneRedundant] and validation passes. Only a real
// deployment fails.
//
// Enabling HA therefore requires moving databaseSkuName and databaseTier to
// GeneralPurpose, which is roughly an order of magnitude more expensive. That
// is a cost decision, so it is left explicit rather than defaulted on.
@description('Database high availability. Requires GeneralPurpose or MemoryOptimized; Burstable rejects it.')
@allowed(['Disabled', 'SameZone', 'ZoneRedundant'])
param databaseHighAvailability string = 'Disabled'

@description('Database compute size. Burstable is adequate for the current request rate.')
param databaseSkuName string = 'Standard_B1ms'

@description('Database tier matching databaseSkuName.')
@allowed(['Burstable', 'GeneralPurpose', 'MemoryOptimized'])
param databaseTier string = 'Burstable'

var suffix = uniqueString(resourceGroup().id, environmentName)
var namePrefix = 'haechi-${environmentName}'
var databaseName = 'haechi_pilot'
var registryName = 'haechi${environmentName}${suffix}'
var containerAppName = '${namePrefix}-website'
var acrPullRoleId = '7f951dda-4ed3-4680-a7ca-43fe172d538d'

// ---------------------------------------------------------------------------
// Observability
// ---------------------------------------------------------------------------

resource logAnalytics 'Microsoft.OperationalInsights/workspaces@2023-09-01' = {
  name: '${namePrefix}-logs'
  location: location
  properties: {
    sku: {
      name: 'PerGB2018'
    }
    retentionInDays: 30
  }
}

// ---------------------------------------------------------------------------
// Network
// ---------------------------------------------------------------------------

resource virtualNetwork 'Microsoft.Network/virtualNetworks@2024-05-01' = {
  name: '${namePrefix}-vnet'
  location: location
  properties: {
    addressSpace: {
      addressPrefixes: ['10.20.0.0/16']
    }
    subnets: [
      {
        // Workload-profile Container Apps environments require at least /27.
        name: 'snet-apps'
        properties: {
          addressPrefix: '10.20.0.0/27'
          delegations: [
            {
              name: 'container-apps'
              properties: {
                serviceName: 'Microsoft.App/environments'
              }
            }
          ]
        }
      }
      {
        name: 'snet-postgres'
        properties: {
          addressPrefix: '10.20.1.0/24'
          delegations: [
            {
              name: 'postgres-flexible'
              properties: {
                serviceName: 'Microsoft.DBforPostgreSQL/flexibleServers'
              }
            }
          ]
        }
      }
    ]
  }
}

resource postgresDnsZone 'Microsoft.Network/privateDnsZones@2020-06-01' = {
  name: '${namePrefix}.private.postgres.database.azure.com'
  location: 'global'
}

resource postgresDnsLink 'Microsoft.Network/privateDnsZones/virtualNetworkLinks@2020-06-01' = {
  parent: postgresDnsZone
  name: '${namePrefix}-dns-link'
  location: 'global'
  properties: {
    registrationEnabled: false
    virtualNetwork: {
      id: virtualNetwork.id
    }
  }
}

// ---------------------------------------------------------------------------
// Identity and registry
// ---------------------------------------------------------------------------

resource identity 'Microsoft.ManagedIdentity/userAssignedIdentities@2023-01-31' = {
  name: '${namePrefix}-identity'
  location: location
}

resource registry 'Microsoft.ContainerRegistry/registries@2023-07-01' = {
  name: registryName
  location: location
  sku: {
    name: 'Standard'
  }
  properties: {
    adminUserEnabled: false
  }
}

// Lets the container app pull images without a registry password.
resource acrPull 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: registry
  name: guid(registry.id, identity.id, acrPullRoleId)
  properties: {
    roleDefinitionId: subscriptionResourceId(
      'Microsoft.Authorization/roleDefinitions',
      acrPullRoleId
    )
    principalId: identity.properties.principalId
    principalType: 'ServicePrincipal'
  }
}

// ---------------------------------------------------------------------------
// Database
// ---------------------------------------------------------------------------

resource postgres 'Microsoft.DBforPostgreSQL/flexibleServers@2024-08-01' = {
  name: '${namePrefix}-postgres'
  location: location
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
      geoRedundantBackup: geoRedundantBackup
    }
    highAvailability: {
      mode: databaseHighAvailability
    }
    network: {
      // No public endpoint. Reachable only from the delegated subnet.
      delegatedSubnetResourceId: virtualNetwork.properties.subnets[1].id
      privateDnsZoneArmResourceId: postgresDnsZone.id
      publicNetworkAccess: 'Disabled'
    }
  }
  dependsOn: [
    postgresDnsLink
  ]
}

resource pilotDatabase 'Microsoft.DBforPostgreSQL/flexibleServers/databases@2024-08-01' = {
  parent: postgres
  name: databaseName
  properties: {
    charset: 'UTF8'
    collation: 'en_US.utf8'
  }
}

// Rejects any connection that does not negotiate TLS.
resource requireTls 'Microsoft.DBforPostgreSQL/flexibleServers/configurations@2024-08-01' = {
  parent: postgres
  name: 'require_secure_transport'
  properties: {
    value: 'ON'
    source: 'user-override'
  }
}

// ---------------------------------------------------------------------------
// Container Apps
// ---------------------------------------------------------------------------

resource containerEnvironment 'Microsoft.App/managedEnvironments@2024-03-01' = {
  name: '${namePrefix}-env'
  location: location
  properties: {
    appLogsConfiguration: {
      destination: 'log-analytics'
      logAnalyticsConfiguration: {
        customerId: logAnalytics.properties.customerId
        sharedKey: logAnalytics.listKeys().primarySharedKey
      }
    }
    vnetConfiguration: {
      infrastructureSubnetId: virtualNetwork.properties.subnets[0].id
      internal: false
    }
    workloadProfiles: [
      {
        name: 'Consumption'
        workloadProfileType: 'Consumption'
      }
    ]
    zoneRedundant: false
  }
}

resource website 'Microsoft.App/containerApps@2024-03-01' = {
  name: containerAppName
  location: location
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${identity.id}': {}
    }
  }
  properties: {
    managedEnvironmentId: containerEnvironment.id
    workloadProfileName: 'Consumption'
    configuration: {
      activeRevisionsMode: 'Single'
      ingress: {
        external: true
        targetPort: 4173
        transport: 'auto'
        allowInsecure: false
        traffic: [
          {
            latestRevision: true
            weight: 100
          }
        ]
      }
      registries: [
        {
          server: registry.properties.loginServer
          identity: identity.id
        }
      ]
      secrets: [
        {
          name: 'database-url'
          value: 'postgresql://${databaseAdministratorLogin}:${uriComponent(databaseAdministratorPassword)}@${postgres.properties.fullyQualifiedDomainName}:5432/${databaseName}?sslmode=verify-full'
        }
        {
          name: 'pilot-ip-salt'
          value: pilotIpSalt
        }
      ]
    }
    template: {
      containers: [
        {
          name: 'website'
          image: containerImage
          resources: {
            cpu: json('0.5')
            memory: '1Gi'
          }
          env: [
            {
              name: 'NODE_ENV'
              value: 'production'
            }
            {
              name: 'HAECHI_SITE_HOST'
              value: '0.0.0.0'
            }
            {
              name: 'HAECHI_SITE_PORT'
              value: '4173'
            }
            {
              // Container Apps ingress sets X-Forwarded-For, so the origin can
              // trust it for rate limiting.
              name: 'HAECHI_TRUST_PROXY'
              value: 'true'
            }
            {
              name: 'HAECHI_ALLOWED_ORIGINS'
              value: allowedOrigins
            }
            {
              name: 'HAECHI_DATABASE_URL'
              secretRef: 'database-url'
            }
            {
              name: 'HAECHI_PILOT_IP_SALT'
              secretRef: 'pilot-ip-salt'
            }
          ]
          probes: [
            {
              type: 'Liveness'
              httpGet: {
                path: '/api/health'
                port: 4173
              }
              initialDelaySeconds: 10
              periodSeconds: 30
              failureThreshold: 3
            }
            {
              // Readiness fails when the database is unreachable, so a broken
              // revision never receives traffic.
              type: 'Readiness'
              httpGet: {
                path: '/api/health'
                port: 4173
              }
              initialDelaySeconds: 5
              periodSeconds: 10
              failureThreshold: 3
            }
          ]
        }
      ]
      scale: {
        minReplicas: minReplicas
        maxReplicas: maxReplicas
        rules: [
          {
            name: 'http-concurrency'
            http: {
              metadata: {
                concurrentRequests: '100'
              }
            }
          }
        ]
      }
    }
  }
  dependsOn: [
    acrPull
    pilotDatabase
    requireTls
  ]
}

// The database has no public endpoint, so migrations cannot run from a CI
// runner. This job runs the same image inside the virtual network and is
// triggered by the deployment workflow before traffic shifts.
resource migrationJob 'Microsoft.App/jobs@2024-03-01' = {
  name: '${namePrefix}-migrate'
  location: location
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${identity.id}': {}
    }
  }
  properties: {
    environmentId: containerEnvironment.id
    workloadProfileName: 'Consumption'
    configuration: {
      triggerType: 'Manual'
      replicaTimeout: 600
      replicaRetryLimit: 1
      manualTriggerConfig: {
        parallelism: 1
        replicaCompletionCount: 1
      }
      registries: [
        {
          server: registry.properties.loginServer
          identity: identity.id
        }
      ]
      secrets: [
        {
          name: 'database-url'
          value: 'postgresql://${databaseAdministratorLogin}:${uriComponent(databaseAdministratorPassword)}@${postgres.properties.fullyQualifiedDomainName}:5432/${databaseName}?sslmode=verify-full'
        }
      ]
    }
    template: {
      containers: [
        {
          name: 'migrate'
          image: containerImage
          command: ['node']
          args: ['dist/migrate.mjs']
          resources: {
            cpu: json('0.25')
            memory: '0.5Gi'
          }
          env: [
            {
              name: 'HAECHI_DATABASE_URL'
              secretRef: 'database-url'
            }
          ]
        }
      ]
    }
  }
  dependsOn: [
    acrPull
    pilotDatabase
    requireTls
  ]
}

@description('Public HTTPS endpoint of the website API.')
output apiBaseUrl string = 'https://${website.properties.configuration.ingress.fqdn}'

@description('Name of the migration job, triggered before traffic shifts.')
output migrationJobName string = migrationJob.name

@description('Login server for the container registry.')
output registryLoginServer string = registry.properties.loginServer

@description('Container registry resource name.')
output registryName string = registry.name

@description('Fully qualified domain name of the PostgreSQL server.')
output postgresFqdn string = postgres.properties.fullyQualifiedDomainName

@description('Name of the pilot database.')
output databaseName string = databaseName

@description('Container app name, used by the deployment workflow.')
output containerAppName string = website.name
