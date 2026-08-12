import * as pulumi from '@pulumi/pulumi'
import * as random from '@pulumi/random'
import { loadConfig } from './config'
import {
  Deployment,
  Domain,
  Project,
  Service,
  Volume,
  electricDatabaseUrl,
  postgresStartCommand,
  webStartCommand,
} from './railway'

const cfg = loadConfig()
const project = new Project('adenosine', {
  name: `adenosine-${pulumi.getStack()}`,
  workspaceId: cfg.workspaceId,
})
const common = { projectId: project.id, environmentId: project.environmentId, region: cfg.region }
const postgresPassword = new random.RandomPassword('postgres-password', {
  length: 32,
  special: false,
})
const electricSecret = new random.RandomPassword('electric-secret', { length: 48, special: false })
const electricDatabasePassword = new random.RandomPassword('electric-database-password', {
  length: 32,
  special: false,
})
const tapPassword = new random.RandomPassword('tap-password', { length: 32, special: false })

const postgres = new Service('postgres', {
  ...common,
  name: 'postgres',
  image:
    'postgres:17.5-alpine3.21@sha256:5d004e058f520673f1f6edbad6b1603d5dab4c818e257c041889ef64672a8cc4',
  startCommand: postgresStartCommand,
  variables: {
    POSTGRES_USER: 'adenosine',
    POSTGRES_DB: 'adenosine',
    POSTGRES_PASSWORD: postgresPassword.result,
  },
})
const postgresVolume = new Volume(
  'postgres-data',
  {
    ...common,
    serviceId: postgres.id,
    mountPath: '/var/lib/postgresql/data',
  },
  { protect: true },
)
const postgresDeployment = new Deployment(
  'postgres-deployment',
  {
    serviceId: postgres.id,
    environmentId: project.environmentId,
    revision:
      'postgres:17.5-alpine3.21@sha256:5d004e058f520673f1f6edbad6b1603d5dab4c818e257c041889ef64672a8cc4',
  },
  { dependsOn: postgresVolume },
)

const collector = new Service('otel-collector', {
  ...common,
  name: 'otel-collector',
  image:
    'otel/opentelemetry-collector-contrib:0.132.0@sha256:e0f565815b2b8e78eb9fdbb80f6190c921ab323aaa8ccceab3255c6d4225f4af',
  startCommand: 'otelcol-contrib --config=env:OTEL_CONFIG',
  variables: {
    OTEL_CONFIG:
      'receivers:\n  otlp:\n    protocols:\n      http:\n        endpoint: 0.0.0.0:4318\nprocessors:\n  batch: {}\nexporters:\n  debug:\n    verbosity: basic\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      processors: [batch]\n      exporters: [debug]\n    metrics:\n      receivers: [otlp]\n      processors: [batch]\n      exporters: [debug]',
  },
})
const collectorDeployment = new Deployment('collector-deployment', {
  serviceId: collector.id,
  environmentId: project.environmentId,
  revision:
    'otel/opentelemetry-collector-contrib:0.132.0@sha256:e0f565815b2b8e78eb9fdbb80f6190c921ab323aaa8ccceab3255c6d4225f4af',
})

const databaseUrl = pulumi.interpolate`postgresql://adenosine:${postgresPassword.result}@postgres.railway.internal:5432/adenosine`
const appVariables: Record<string, pulumi.Input<string>> = {
  DATABASE_URL: databaseUrl,
  ADENOSINE_BASE_URL: `https://${cfg.domain}`,
  ADENOSINE_LISTEN_ADDR: ':8080',
  ADENOSINE_REPO_ROOT: '/var/lib/adenosine/repos',
  ADENOSINE_GIT_BINARY: 'git',
  ADENOSINE_SSH_LISTEN_ADDR: ':2222',
  ADENOSINE_SSH_HOST: cfg.domain,
  ADENOSINE_SSH_PORT: '2222',
  ADENOSINE_SSH_HOST_KEY_PATH: '/var/lib/adenosine/state/ssh_host_ed25519_key',
  ADENOSINE_OAUTH_STATE_KEY: cfg.oauthStateKey,
  ADENOSINE_OAUTH_CREDENTIAL_KEY: cfg.oauthCredentialKey,
  OTEL_SERVICE_NAME: 'adenosine',
  OTEL_EXPORTER_OTLP_ENDPOINT: 'http://otel-collector.railway.internal:4318',
  OTEL_EXPORTER_OTLP_PROTOCOL: 'http/protobuf',
  OTEL_RESOURCE_ATTRIBUTES: `deployment.environment.name=production,service.version=${cfg.image.split('@sha256:')[1]}`,
}

if (cfg.enableElectric) {
  appVariables.ADENOSINE_ELECTRIC_URL = 'http://electric.railway.internal:3000'
  appVariables.ADENOSINE_ELECTRIC_SECRET = electricSecret.result
  const electricURL = electricDatabaseUrl(electricDatabasePassword.result)
  const electric = new Service('electric', {
    ...common,
    name: 'electric',
    image:
      'docker.io/electricsql/electric:1.7.10@sha256:5758f72b40d2ea9c3ba676b0f6070dd5cd5552975c19b2b8f82206743f47164c',
    variables: {
      DATABASE_URL: electricURL,
      ELECTRIC_SECRET: electricSecret.result,
      ELECTRIC_PORT: '3000',
      ELECTRIC_MANUAL_TABLE_PUBLISHING: 'true',
    },
  })
  const electricDeployment = new Deployment(
    'electric-deployment',
    {
      serviceId: electric.id,
      environmentId: project.environmentId,
      revision:
        'docker.io/electricsql/electric:1.7.10@sha256:5758f72b40d2ea9c3ba676b0f6070dd5cd5552975c19b2b8f82206743f47164c',
    },
    { dependsOn: postgresDeployment },
  )
  void electricDeployment
}
if (cfg.enableTap) {
  appVariables.ADENOSINE_TAP_CONSUMER = 'tap:dev.adenosine:v1'
  appVariables.ADENOSINE_TAP_ADMIN_PASSWORD = tapPassword.result
}
appVariables.RAILWAY_RUN_UID = '0'
if (cfg.enableElectric) appVariables.ELECTRIC_DATABASE_PASSWORD = electricDatabasePassword.result

const app = new Service(
  'app',
  {
    ...common,
    name: 'adenosine',
    image: cfg.image,
    healthcheckPath: '/health/ready',
    preDeployCommand: [
      cfg.enableElectric
        ? `adenosine migrate && psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -v electric_password="$ELECTRIC_DATABASE_PASSWORD" <<'SQL'
SELECT 'CREATE ROLE electric LOGIN REPLICATION' WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname='electric') \\gexec
ALTER ROLE electric WITH LOGIN REPLICATION NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT PASSWORD :'electric_password';
GRANT USAGE ON SCHEMA network TO electric;
GRANT SELECT ON network.repositories, network.profiles, network.stars, network.issues, network.issue_comments, network.pull_requests, network.pull_request_reviews TO electric;
ALTER TABLE network.repositories REPLICA IDENTITY FULL; ALTER TABLE network.profiles REPLICA IDENTITY FULL; ALTER TABLE network.stars REPLICA IDENTITY FULL; ALTER TABLE network.issues REPLICA IDENTITY FULL; ALTER TABLE network.issue_comments REPLICA IDENTITY FULL; ALTER TABLE network.pull_requests REPLICA IDENTITY FULL; ALTER TABLE network.pull_request_reviews REPLICA IDENTITY FULL;
SELECT 'CREATE PUBLICATION electric_publication_default' WHERE NOT EXISTS (SELECT FROM pg_publication WHERE pubname='electric_publication_default') \\gexec
ALTER PUBLICATION electric_publication_default SET TABLE network.repositories, network.profiles, network.stars, network.issues, network.issue_comments, network.pull_requests, network.pull_request_reviews;
SQL`
        : 'adenosine migrate',
    ],
    startCommand:
      '/bin/sh -ec \'install -d -o adenosine -g adenosine -m 0750 /var/lib/adenosine/repos /var/lib/adenosine/state; test -f /var/lib/adenosine/state/ssh_host_ed25519_key || su adenosine -s /bin/sh -c "ssh-keygen -q -t ed25519 -N \\\"\\\" -f /var/lib/adenosine/state/ssh_host_ed25519_key"; chown -R adenosine:adenosine /var/lib/adenosine; chmod 600 /var/lib/adenosine/state/ssh_host_ed25519_key; exec su adenosine -s /bin/sh -c "exec adenosine serve"\'',
    variables: appVariables,
  },
  { dependsOn: [postgresDeployment, collectorDeployment] },
)
const appVolume = new Volume(
  'adenosine-data',
  {
    ...common,
    serviceId: app.id,
    mountPath: '/var/lib/adenosine',
  },
  { protect: true },
)
const appDeployment = new Deployment(
  'app-deployment',
  { serviceId: app.id, environmentId: project.environmentId, revision: cfg.image },
  { dependsOn: appVolume },
)
const web = new Service('web', {
  ...common,
  name: 'web',
  image: cfg.image,
  startCommand: webStartCommand,
  healthcheckPath: '/',
  variables: { ADENOSINE_INTERNAL_API_URL: 'http://adenosine.railway.internal:8080', PORT: '3000' },
})
const webDeployment = new Deployment(
  'web-deployment',
  { serviceId: web.id, environmentId: project.environmentId, revision: cfg.image },
  { dependsOn: appDeployment },
)
const gateway = new Service('gateway', {
  ...common,
  name: 'gateway',
  image:
    'docker.io/library/caddy:2.11.4-alpine@sha256:5f5c8640aae01df9654968d946d8f1a56c497f1dd5c5cda4cf95ab7c14d58648',
  healthcheckPath: '/health/ready',
  startCommand: `/bin/sh -ec 'cat > /tmp/Caddyfile <<"EOF"
:8080 {
  header X-Adenosine-Image-Digest "${cfg.image.split('@sha256:')[1]}"
  @internal path /internal/*
  respond @internal 404
  @backend path /api/* /oauth/* /health/* /docs/api /openapi.json /openapi.yaml
  @git path_regexp git ^/[^/]+/[^/]+\\.git/(info/refs|git-upload-pack|git-receive-pack)$
  handle @git { reverse_proxy adenosine.railway.internal:8080 { flush_interval -1 } }
  handle @backend { reverse_proxy adenosine.railway.internal:8080 }
  handle { reverse_proxy web.railway.internal:3000 }
}
EOF
exec caddy run --config /tmp/Caddyfile --adapter caddyfile'`,
  variables: {},
})
const gatewayDeployment = new Deployment(
  'gateway-deployment',
  {
    serviceId: gateway.id,
    environmentId: project.environmentId,
    revision:
      'docker.io/library/caddy:2.11.4-alpine@sha256:5f5c8640aae01df9654968d946d8f1a56c497f1dd5c5cda4cf95ab7c14d58648',
  },
  { dependsOn: webDeployment },
)
const webDomain = new Domain(
  'web-domain',
  {
    projectId: project.id,
    environmentId: project.environmentId,
    serviceId: gateway.id,
    domain: cfg.domain,
    targetPort: 8080,
  },
  { dependsOn: gatewayDeployment },
)

if (cfg.enableTap) {
  const tap = new Service(
    'tap',
    {
      ...common,
      name: 'tap',
      image:
        'ghcr.io/bluesky-social/indigo/tap:0.1.10@sha256:5e20bfe416d29fcd215ed8bf99f10b2ab825a6de4e5599846dd33967ade2abeb',
      variables: {
        TAP_DATABASE_URL: 'sqlite:///data/tap.db',
        TAP_SIGNAL_COLLECTION: 'dev.adenosine.repo',
        TAP_COLLECTION_FILTERS: 'dev.adenosine.*',
        TAP_WEBHOOK_URL: 'http://adenosine.railway.internal:8080/internal/federation/tap',
        TAP_ADMIN_PASSWORD: tapPassword.result,
      },
    },
    { dependsOn: app },
  )
  const tapVolume = new Volume(
    'tap-data',
    { ...common, serviceId: tap.id, mountPath: '/data' },
    { protect: true },
  )
  const tapDeployment = new Deployment(
    'tap-deployment',
    {
      serviceId: tap.id,
      environmentId: project.environmentId,
      revision:
        'ghcr.io/bluesky-social/indigo/tap:0.1.10@sha256:5e20bfe416d29fcd215ed8bf99f10b2ab825a6de4e5599846dd33967ade2abeb',
    },
    { dependsOn: tapVolume },
  )
  void tapDeployment
}

void postgresVolume
void appVolume
void webDomain

export const projectId = project.id
export const environmentId = project.environmentId
export const webUrl = `https://${cfg.domain}`
export const gitHttpsBase = `https://${cfg.domain}`
export const apiDocsUrl = `https://${cfg.domain}/docs/api`
export const healthUrl = `https://${cfg.domain}/health/ready`
export const image = cfg.image
// Railway custom domains expose HTTP only. Configure a Railway TCP proxy manually before enabling SSH conformance.
export const sshHost: string | null = null
