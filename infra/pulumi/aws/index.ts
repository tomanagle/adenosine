import * as aws from '@pulumi/aws'
import * as pulumi from '@pulumi/pulumi'
import * as random from '@pulumi/random'
import { loadConfig } from './config'

const cfg = loadConfig()
const name = `adenosine-${pulumi.getStack()}`
const tags = { Application: 'adenosine', Stack: pulumi.getStack(), ManagedBy: 'pulumi' }
const azs = aws
  .getAvailabilityZonesOutput({ state: 'available' })
  .names.apply((names) => names.slice(0, 2))

const vpc = new aws.ec2.Vpc('vpc', {
  cidrBlock: '10.42.0.0/16',
  enableDnsHostnames: true,
  enableDnsSupport: true,
  tags: { ...tags, Name: name },
})
const internetGateway = new aws.ec2.InternetGateway('internet-gateway', { vpcId: vpc.id, tags })
const publicRouteTable = new aws.ec2.RouteTable('public-routes', {
  vpcId: vpc.id,
  routes: [{ cidrBlock: '0.0.0.0/0', gatewayId: internetGateway.id }],
  tags,
})
const publicSubnets = [0, 1].map((index) => {
  const subnet = new aws.ec2.Subnet(`public-${index}`, {
    vpcId: vpc.id,
    availabilityZone: azs.apply((values) => values[index]),
    cidrBlock: `10.42.${index}.0/24`,
    mapPublicIpOnLaunch: true,
    tags,
  })
  const routeAssociation = new aws.ec2.RouteTableAssociation(`public-route-${index}`, {
    subnetId: subnet.id,
    routeTableId: publicRouteTable.id,
  })
  void routeAssociation
  return subnet
})
const privateSubnets = [0, 1].map(
  (index) =>
    new aws.ec2.Subnet(`private-${index}`, {
      vpcId: vpc.id,
      availabilityZone: azs.apply((values) => values[index]),
      cidrBlock: `10.42.${index + 10}.0/24`,
      tags,
    }),
)
const natAddress = new aws.ec2.Eip(
  'nat-address',
  { domain: 'vpc', tags },
  { dependsOn: internetGateway },
)
const natGateway = new aws.ec2.NatGateway('nat', {
  allocationId: natAddress.id,
  subnetId: publicSubnets[0].id,
  tags,
})
const privateRouteTable = new aws.ec2.RouteTable('private-routes', {
  vpcId: vpc.id,
  routes: [{ cidrBlock: '0.0.0.0/0', natGatewayId: natGateway.id }],
  tags,
})
privateSubnets.forEach(
  (subnet, index) =>
    new aws.ec2.RouteTableAssociation(`private-route-${index}`, {
      subnetId: subnet.id,
      routeTableId: privateRouteTable.id,
    }),
)

const albSg = new aws.ec2.SecurityGroup('alb-sg', {
  vpcId: vpc.id,
  ingress: [
    {
      protocol: 'tcp',
      fromPort: 443,
      toPort: 443,
      cidrBlocks: ['0.0.0.0/0'],
      ipv6CidrBlocks: ['::/0'],
    },
  ],
  egress: [{ protocol: '-1', fromPort: 0, toPort: 0, cidrBlocks: ['0.0.0.0/0'] }],
  tags,
})
const taskSg = new aws.ec2.SecurityGroup('task-sg', {
  vpcId: vpc.id,
  ingress: [
    { protocol: 'tcp', fromPort: 8080, toPort: 8080, securityGroups: [albSg.id] },
    { protocol: 'tcp', fromPort: 2222, toPort: 2222, cidrBlocks: cfg.sshAllowedCidrs },
    { protocol: 'tcp', fromPort: 2222, toPort: 2222, cidrBlocks: ['10.42.0.0/16'] },
  ],
  egress: [{ protocol: '-1', fromPort: 0, toPort: 0, cidrBlocks: ['0.0.0.0/0'] }],
  tags,
})
const databaseSg = new aws.ec2.SecurityGroup('database-sg', {
  vpcId: vpc.id,
  ingress: [{ protocol: 'tcp', fromPort: 5432, toPort: 5432, securityGroups: [taskSg.id] }],
  tags,
})
const efsSg = new aws.ec2.SecurityGroup('efs-sg', {
  vpcId: vpc.id,
  ingress: [{ protocol: 'tcp', fromPort: 2049, toPort: 2049, securityGroups: [taskSg.id] }],
  tags,
})

const dbSubnetGroup = new aws.rds.SubnetGroup('database-subnets', {
  subnetIds: privateSubnets.map((subnet) => subnet.id),
  tags,
})
const databaseParameters = new aws.rds.ParameterGroup('database-parameters', {
  family: 'postgres17',
  parameters: [
    {
      name: 'rds.logical_replication',
      value: cfg.enableElectric ? '1' : '0',
      applyMethod: 'pending-reboot',
    },
    {
      name: 'max_replication_slots',
      value: cfg.enableElectric ? '5' : '0',
      applyMethod: 'pending-reboot',
    },
    {
      name: 'max_wal_senders',
      value: cfg.enableElectric ? '5' : '0',
      applyMethod: 'pending-reboot',
    },
  ],
  tags,
})
const dbPassword = new random.RandomPassword('database-password', { length: 32, special: false })
const database = new aws.rds.Instance(
  'database',
  {
    identifier: name,
    engine: 'postgres',
    engineVersion: '17.5',
    instanceClass: cfg.databaseInstanceClass,
    allocatedStorage: cfg.databaseAllocatedStorage,
    maxAllocatedStorage: Math.max(100, cfg.databaseAllocatedStorage * 2),
    storageEncrypted: true,
    dbName: 'adenosine',
    username: 'adenosine',
    password: dbPassword.result,
    dbSubnetGroupName: dbSubnetGroup.name,
    parameterGroupName: databaseParameters.name,
    vpcSecurityGroupIds: [databaseSg.id],
    backupRetentionPeriod: cfg.backupRetentionDays,
    deletionProtection: true,
    skipFinalSnapshot: false,
    finalSnapshotIdentifier: `${name}-final`,
    applyImmediately: false,
    multiAz: false,
    publiclyAccessible: false,
    tags,
  },
  { protect: true },
)

const filesystem = new aws.efs.FileSystem(
  'git-storage',
  {
    encrypted: true,
    performanceMode: cfg.repositoryPerformanceMode,
    throughputMode: 'bursting',
    lifecyclePolicies: [{ transitionToIa: 'AFTER_30_DAYS' }],
    tags,
  },
  { protect: true },
)
privateSubnets.forEach(
  (subnet, index) =>
    new aws.efs.MountTarget(`efs-${index}`, {
      fileSystemId: filesystem.id,
      subnetId: subnet.id,
      securityGroups: [efsSg.id],
    }),
)
const accessPoint = new aws.efs.AccessPoint('adenosine-data', {
  fileSystemId: filesystem.id,
  posixUser: { uid: 100, gid: 101 },
  rootDirectory: {
    path: '/adenosine',
    creationInfo: { ownerUid: 100, ownerGid: 101, permissions: '0750' },
  },
  tags,
})

const cluster = new aws.ecs.Cluster('cluster', {
  name,
  settings: [{ name: 'containerInsights', value: 'enabled' }],
  tags,
})
const instanceRole = new aws.iam.Role('ecs-instance-role', {
  assumeRolePolicy: aws.iam.assumeRolePolicyForPrincipal({ Service: 'ec2.amazonaws.com' }),
  tags,
})
const ecsInstancePolicy = new aws.iam.RolePolicyAttachment('ecs-instance-policy', {
  role: instanceRole.name,
  policyArn: aws.iam.ManagedPolicy.AmazonEC2ContainerServiceforEC2Role,
})
const ssmInstancePolicy = new aws.iam.RolePolicyAttachment('ssm-instance-policy', {
  role: instanceRole.name,
  policyArn: aws.iam.ManagedPolicy.AmazonSSMManagedInstanceCore,
})
const instanceProfile = new aws.iam.InstanceProfile('ecs-instance-profile', {
  role: instanceRole.name,
  tags,
})
const ecsAmi = aws.ssm.getParameterOutput({
  name: '/aws/service/ecs/optimized-ami/amazon-linux-2023/recommended/image_id',
}).value
const launchTemplate = new aws.ec2.LaunchTemplate('ecs-host', {
  imageId: ecsAmi,
  instanceType: cfg.instanceType,
  iamInstanceProfile: { arn: instanceProfile.arn },
  vpcSecurityGroupIds: [taskSg.id],
  userData:
    pulumi.interpolate`#!/bin/bash\necho ECS_CLUSTER=${cluster.name} >> /etc/ecs/ecs.config\necho ECS_ENABLE_AWSLOGS_EXECUTIONROLE_OVERRIDE=true >> /etc/ecs/ecs.config\n`.apply(
      (value) => Buffer.from(value).toString('base64'),
    ),
  blockDeviceMappings: [
    { deviceName: '/dev/xvda', ebs: { encrypted: 'true', volumeSize: 30, volumeType: 'gp3' } },
  ],
  metadataOptions: { httpTokens: 'required', httpEndpoint: 'enabled' },
  tags,
})
const asg = new aws.autoscaling.Group('ecs-hosts', {
  minSize: 1,
  maxSize: 2,
  desiredCapacity: 1,
  vpcZoneIdentifiers: publicSubnets.map((subnet) => subnet.id),
  launchTemplate: { id: launchTemplate.id, version: '$Latest' },
  tags: Object.entries(tags).map(([key, value]) => ({ key, value, propagateAtLaunch: true })),
})
const capacityProvider = new aws.ecs.CapacityProvider('capacity', {
  autoScalingGroupProvider: {
    autoScalingGroupArn: asg.arn,
    managedScaling: { status: 'ENABLED', targetCapacity: 100 },
    managedTerminationProtection: 'DISABLED',
  },
  tags,
})
const clusterCapacity = new aws.ecs.ClusterCapacityProviders('cluster-capacity', {
  clusterName: cluster.name,
  capacityProviders: [capacityProvider.name],
  defaultCapacityProviderStrategies: [
    { capacityProvider: capacityProvider.name, weight: 1, base: 1 },
  ],
})

const executionRole = new aws.iam.Role('task-execution-role', {
  assumeRolePolicy: aws.iam.assumeRolePolicyForPrincipal({ Service: 'ecs-tasks.amazonaws.com' }),
  tags,
})
const taskExecutionPolicy = new aws.iam.RolePolicyAttachment('task-execution-policy', {
  role: executionRole.name,
  policyArn: aws.iam.ManagedPolicy.AmazonECSTaskExecutionRolePolicy,
})
const taskRole = new aws.iam.Role('task-role', {
  assumeRolePolicy: aws.iam.assumeRolePolicyForPrincipal({ Service: 'ecs-tasks.amazonaws.com' }),
  tags,
})
const taskEfsPolicy = new aws.iam.RolePolicy('task-efs-policy', {
  role: taskRole.id,
  policy: pulumi.all([filesystem.arn, accessPoint.arn]).apply(([fs, ap]) =>
    JSON.stringify({
      Version: '2012-10-17',
      Statement: [
        {
          Effect: 'Allow',
          Action: ['elasticfilesystem:ClientMount', 'elasticfilesystem:ClientWrite'],
          Resource: fs,
          Condition: { StringEquals: { 'elasticfilesystem:AccessPointArn': ap } },
        },
      ],
    }),
  ),
})
const taskExecChannelPolicy = new aws.iam.RolePolicy('task-exec-channel-policy', {
  role: taskRole.id,
  policy: JSON.stringify({
    Version: '2012-10-17',
    Statement: [
      {
        Effect: 'Allow',
        Action: [
          'ssmmessages:CreateControlChannel',
          'ssmmessages:CreateDataChannel',
          'ssmmessages:OpenControlChannel',
          'ssmmessages:OpenDataChannel',
        ],
        Resource: '*',
      },
    ],
  }),
})
const oauthState = new aws.secretsmanager.Secret('oauth-state', { tags })
const oauthCredential = new aws.secretsmanager.Secret('oauth-credential', { tags })
const databaseUrlSecret = new aws.secretsmanager.Secret('database-url', { tags })
const electricSecretValue = new random.RandomPassword('electric-secret', {
  length: 48,
  special: false,
})
const electricDatabasePassword = new random.RandomPassword('electric-database-password', {
  length: 32,
  special: false,
})
const tapPasswordValue = new random.RandomPassword('tap-password', { length: 32, special: false })
const electricSecret = new aws.secretsmanager.Secret('electric-secret', { tags })
const electricDatabasePasswordSecret = new aws.secretsmanager.Secret('electric-database-password', {
  tags,
})
const electricDatabaseUrlSecret = new aws.secretsmanager.Secret('electric-database-url', { tags })
const tapPassword = new aws.secretsmanager.Secret('tap-password', { tags })
const oauthStateValue = new aws.secretsmanager.SecretVersion('oauth-state-value', {
  secretId: oauthState.id,
  secretString: cfg.oauthStateKey,
})
const oauthCredentialValue = new aws.secretsmanager.SecretVersion('oauth-credential-value', {
  secretId: oauthCredential.id,
  secretString: cfg.oauthCredentialKey,
})
const databaseUrlValue = new aws.secretsmanager.SecretVersion('database-url-value', {
  secretId: databaseUrlSecret.id,
  secretString: pulumi.interpolate`postgresql://adenosine:${dbPassword.result}@${database.address}:5432/adenosine?sslmode=require`,
})
const electricSecretVersion = new aws.secretsmanager.SecretVersion('electric-secret-value', {
  secretId: electricSecret.id,
  secretString: electricSecretValue.result,
})
const electricDatabasePasswordVersion = new aws.secretsmanager.SecretVersion(
  'electric-database-password-value',
  {
    secretId: electricDatabasePasswordSecret.id,
    secretString: electricDatabasePassword.result,
  },
)
const electricDatabaseUrlValue = new aws.secretsmanager.SecretVersion(
  'electric-database-url-value',
  {
    secretId: electricDatabaseUrlSecret.id,
    secretString: pulumi.interpolate`postgresql://electric:${electricDatabasePassword.result}@${database.address}:5432/adenosine?sslmode=require`,
  },
)
const tapPasswordVersion = new aws.secretsmanager.SecretVersion('tap-password-value', {
  secretId: tapPassword.id,
  secretString: tapPasswordValue.result,
})
const taskSecretPolicy = new aws.iam.RolePolicy('task-secret-policy', {
  role: executionRole.id,
  policy: pulumi
    .all([
      oauthState.arn,
      oauthCredential.arn,
      databaseUrlSecret.arn,
      electricSecret.arn,
      electricDatabasePasswordSecret.arn,
      electricDatabaseUrlSecret.arn,
      tapPassword.arn,
    ])
    .apply((arns) =>
      JSON.stringify({
        Version: '2012-10-17',
        Statement: [{ Effect: 'Allow', Action: ['secretsmanager:GetSecretValue'], Resource: arns }],
      }),
    ),
})
const backupVault = new aws.backup.Vault('backup-vault', { name: name, tags }, { protect: true })
const backupPlan = new aws.backup.Plan('backup-plan', {
  name,
  rules: [
    {
      ruleName: 'daily',
      targetVaultName: backupVault.name,
      schedule: 'cron(0 3 * * ? *)',
      startWindow: 60,
      completionWindow: 360,
      lifecycle: { deleteAfter: cfg.backupRetentionDays },
      recoveryPointTags: tags,
    },
  ],
  tags,
})
const backupRole = new aws.iam.Role('backup-role', {
  assumeRolePolicy: aws.iam.assumeRolePolicyForPrincipal({ Service: 'backup.amazonaws.com' }),
  tags,
})
const backupPolicy = new aws.iam.RolePolicyAttachment('backup-policy', {
  role: backupRole.name,
  policyArn: aws.iam.ManagedPolicy.AWSBackupServiceRolePolicyForBackup,
})
const backupSelection = new aws.backup.Selection('backup-selection', {
  iamRoleArn: backupRole.arn,
  name,
  planId: backupPlan.id,
  resources: [filesystem.arn, database.arn],
})

const logs = new aws.cloudwatch.LogGroup('logs', {
  name: `/adenosine/${pulumi.getStack()}`,
  retentionInDays: 30,
  tags,
})
const region = aws.getRegionOutput().name
const logConfiguration: aws.ecs.LogConfiguration = {
  logDriver: 'awslogs',
  options: {
    'awslogs-group': logs.name,
    'awslogs-region': region,
    'awslogs-stream-prefix': 'service',
  },
}
const appEnvironment = [
  { name: 'ADENOSINE_BASE_URL', value: `https://${cfg.domain}` },
  { name: 'ADENOSINE_LISTEN_ADDR', value: ':8080' },
  { name: 'ADENOSINE_REPO_ROOT', value: '/var/lib/adenosine/repos' },
  { name: 'ADENOSINE_GIT_BINARY', value: 'git' },
  { name: 'ADENOSINE_SSH_LISTEN_ADDR', value: ':2222' },
  { name: 'ADENOSINE_SSH_HOST', value: `ssh.${cfg.domain}` },
  { name: 'ADENOSINE_SSH_PORT', value: '2222' },
  { name: 'ADENOSINE_SSH_HOST_KEY_PATH', value: '/var/lib/adenosine/state/ssh_host_ed25519_key' },
  { name: 'OTEL_SERVICE_NAME', value: 'adenosine' },
  { name: 'OTEL_EXPORTER_OTLP_ENDPOINT', value: 'http://127.0.0.1:4318' },
  { name: 'OTEL_EXPORTER_OTLP_PROTOCOL', value: 'http/protobuf' },
  {
    name: 'OTEL_RESOURCE_ATTRIBUTES',
    value: `deployment.environment.name=production,service.version=${cfg.image.split('@sha256:')[1]}`,
  },
]
if (cfg.enableElectric)
  appEnvironment.push({ name: 'ADENOSINE_ELECTRIC_URL', value: 'http://127.0.0.1:3000' })
if (cfg.enableTap)
  appEnvironment.push({ name: 'ADENOSINE_TAP_CONSUMER', value: 'tap:dev.adenosine:v1' })
const containers: aws.ecs.ContainerDefinition[] = [
  {
    name: 'migrate',
    image: cfg.image,
    essential: false,
    cpu: 256,
    memory: 512,
    entryPoint: ['/bin/sh', '-ec'],
    command: [
      cfg.enableElectric
        ? `adenosine migrate && psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -v electric_password="$ELECTRIC_DATABASE_PASSWORD" <<'SQL'
SELECT 'CREATE ROLE electric LOGIN' WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname='electric') \\gexec
ALTER ROLE electric WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT PASSWORD :'electric_password';
GRANT rds_replication TO electric;
GRANT USAGE ON SCHEMA network TO electric;
GRANT SELECT ON network.repositories, network.profiles, network.stars, network.issues, network.issue_comments, network.pull_requests, network.pull_request_reviews TO electric;
ALTER TABLE network.repositories REPLICA IDENTITY FULL; ALTER TABLE network.profiles REPLICA IDENTITY FULL; ALTER TABLE network.stars REPLICA IDENTITY FULL; ALTER TABLE network.issues REPLICA IDENTITY FULL; ALTER TABLE network.issue_comments REPLICA IDENTITY FULL; ALTER TABLE network.pull_requests REPLICA IDENTITY FULL; ALTER TABLE network.pull_request_reviews REPLICA IDENTITY FULL;
SELECT 'CREATE PUBLICATION electric_publication_default' WHERE NOT EXISTS (SELECT FROM pg_publication WHERE pubname='electric_publication_default') \\gexec
ALTER PUBLICATION electric_publication_default SET TABLE network.repositories, network.profiles, network.stars, network.issues, network.issue_comments, network.pull_requests, network.pull_request_reviews;
SQL`
        : 'adenosine migrate',
    ],
    secrets: [
      { name: 'DATABASE_URL', valueFrom: databaseUrlValue.arn },
      ...(cfg.enableElectric
        ? [{ name: 'ELECTRIC_DATABASE_PASSWORD', valueFrom: electricDatabasePasswordVersion.arn }]
        : []),
    ],
    logConfiguration,
  },
  {
    name: 'adenosine',
    image: cfg.image,
    essential: true,
    cpu: 768,
    memory: 1536,
    entryPoint: ['/bin/sh', '-ec'],
    dependsOn: [{ containerName: 'migrate', condition: 'SUCCESS' }],
    command: [
      "mkdir -p /var/lib/adenosine/repos /var/lib/adenosine/state; test -f /var/lib/adenosine/state/ssh_host_ed25519_key || ssh-keygen -q -t ed25519 -N '' -f /var/lib/adenosine/state/ssh_host_ed25519_key; chmod 600 /var/lib/adenosine/state/ssh_host_ed25519_key; exec adenosine serve",
    ],
    portMappings: [
      { containerPort: 8080, protocol: 'tcp' },
      { containerPort: 2222, protocol: 'tcp' },
    ],
    environment: appEnvironment,
    secrets: [
      { name: 'DATABASE_URL', valueFrom: electricDatabaseUrlValue.arn },
      { name: 'ADENOSINE_OAUTH_STATE_KEY', valueFrom: oauthStateValue.arn },
      { name: 'ADENOSINE_OAUTH_CREDENTIAL_KEY', valueFrom: oauthCredentialValue.arn },
      ...(cfg.enableElectric
        ? [{ name: 'ADENOSINE_ELECTRIC_SECRET', valueFrom: electricSecretVersion.arn }]
        : []),
      ...(cfg.enableTap
        ? [{ name: 'ADENOSINE_TAP_ADMIN_PASSWORD', valueFrom: tapPasswordVersion.arn }]
        : []),
    ],
    mountPoints: [
      { sourceVolume: 'adenosine-data', containerPath: '/var/lib/adenosine', readOnly: false },
    ],
    healthCheck: {
      command: ['CMD-SHELL', 'wget -q --spider http://127.0.0.1:8080/health/ready || exit 1'],
      interval: 30,
      timeout: 5,
      retries: 5,
      startPeriod: 60,
    },
    logConfiguration,
  },
  {
    name: 'web',
    image: cfg.image,
    essential: true,
    cpu: 256,
    memory: 512,
    command: ['web'],
    dependsOn: [{ containerName: 'adenosine', condition: 'HEALTHY' }],
    portMappings: [{ containerPort: 3000, protocol: 'tcp' }],
    environment: [
      { name: 'ADENOSINE_INTERNAL_API_URL', value: 'http://127.0.0.1:8080' },
      { name: 'PORT', value: '3000' },
    ],
    healthCheck: {
      command: ['CMD-SHELL', 'wget -q --spider http://127.0.0.1:3000/ || exit 1'],
      interval: 30,
      timeout: 5,
      retries: 5,
      startPeriod: 60,
    },
    logConfiguration,
  },
  {
    name: 'gateway',
    image:
      'docker.io/library/caddy:2.11.4-alpine@sha256:5f5c8640aae01df9654968d946d8f1a56c497f1dd5c5cda4cf95ab7c14d58648',
    essential: true,
    cpu: 128,
    memory: 256,
    dependsOn: [
      { containerName: 'adenosine', condition: 'HEALTHY' },
      { containerName: 'web', condition: 'HEALTHY' },
    ],
    entryPoint: ['/bin/sh', '-ec'],
    command: [
      `cat >/tmp/Caddyfile <<'EOF'
:8081 {
  header X-Adenosine-Image-Digest "${cfg.image.split('@sha256:')[1]}"
  @internal path /internal/*
  respond @internal 404
  @backend path /api/* /oauth/* /health/* /docs/api /openapi.json /openapi.yaml
  @git path_regexp git ^/[^/]+/[^/]+\\.git/(info/refs|git-upload-pack|git-receive-pack)$
  handle @git { reverse_proxy 127.0.0.1:8080 { flush_interval -1 } }
  handle @backend { reverse_proxy 127.0.0.1:8080 }
  handle { reverse_proxy 127.0.0.1:3000 }
}
EOF
exec caddy run --config /tmp/Caddyfile --adapter caddyfile`,
    ],
    portMappings: [{ containerPort: 8081, protocol: 'tcp' }],
    healthCheck: {
      command: ['CMD-SHELL', 'wget -q --spider http://127.0.0.1:8081/health/ready || exit 1'],
      interval: 30,
      timeout: 5,
      retries: 5,
      startPeriod: 60,
    },
    logConfiguration,
  },
  {
    name: 'otel-collector',
    image:
      'otel/opentelemetry-collector-contrib:0.132.0@sha256:e0f565815b2b8e78eb9fdbb80f6190c921ab323aaa8ccceab3255c6d4225f4af',
    essential: false,
    cpu: 128,
    memory: 256,
    command: ['--config=env:OTEL_CONFIG'],
    environment: [
      {
        name: 'OTEL_CONFIG',
        value:
          'receivers:\n  otlp:\n    protocols:\n      http:\n        endpoint: 0.0.0.0:4318\nprocessors:\n  batch: {}\nexporters:\n  debug:\n    verbosity: basic\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      processors: [batch]\n      exporters: [debug]\n    metrics:\n      receivers: [otlp]\n      processors: [batch]\n      exporters: [debug]',
      },
    ],
    logConfiguration,
  },
]
if (cfg.enableElectric)
  containers.push({
    name: 'electric',
    image:
      'docker.io/electricsql/electric:1.7.10@sha256:5758f72b40d2ea9c3ba676b0f6070dd5cd5552975c19b2b8f82206743f47164c',
    essential: false,
    cpu: 128,
    memory: 256,
    environment: [
      { name: 'ELECTRIC_PORT', value: '3000' },
      { name: 'ELECTRIC_MANUAL_TABLE_PUBLISHING', value: 'true' },
    ],
    secrets: [
      { name: 'DATABASE_URL', valueFrom: databaseUrlValue.arn },
      { name: 'ELECTRIC_SECRET', valueFrom: electricSecretVersion.arn },
    ],
    logConfiguration,
  })
if (cfg.enableTap)
  containers.push({
    name: 'tap',
    image:
      'ghcr.io/bluesky-social/indigo/tap:0.1.10@sha256:5e20bfe416d29fcd215ed8bf99f10b2ab825a6de4e5599846dd33967ade2abeb',
    essential: false,
    cpu: 128,
    memory: 256,
    environment: [
      { name: 'TAP_DATABASE_URL', value: 'sqlite:///data/tap.db' },
      { name: 'TAP_SIGNAL_COLLECTION', value: 'dev.adenosine.repo' },
      { name: 'TAP_COLLECTION_FILTERS', value: 'dev.adenosine.*' },
      { name: 'TAP_WEBHOOK_URL', value: 'http://127.0.0.1:8080/internal/federation/tap' },
    ],
    secrets: [{ name: 'TAP_ADMIN_PASSWORD', valueFrom: tapPasswordVersion.arn }],
    mountPoints: [{ sourceVolume: 'adenosine-data', containerPath: '/data', readOnly: false }],
    logConfiguration,
  })

const taskDefinition = new aws.ecs.TaskDefinition('task', {
  family: name,
  networkMode: 'awsvpc',
  requiresCompatibilities: ['EC2'],
  cpu: '2048',
  memory: '4096',
  executionRoleArn: executionRole.arn,
  taskRoleArn: taskRole.arn,
  volumes: [
    {
      name: 'adenosine-data',
      efsVolumeConfiguration: {
        fileSystemId: filesystem.id,
        transitEncryption: 'ENABLED',
        authorizationConfig: { accessPointId: accessPoint.id, iam: 'ENABLED' },
      },
    },
  ],
  containerDefinitions: pulumi.jsonStringify(containers),
  tags,
})

const certificate = new aws.acm.Certificate('certificate', {
  domainName: cfg.domain,
  subjectAlternativeNames: [`ssh.${cfg.domain}`],
  validationMethod: 'DNS',
  tags,
})
const validations = certificate.domainValidationOptions.apply((options) =>
  options.map(
    (option, index) =>
      new aws.route53.Record(`certificate-${index}`, {
        zoneId: cfg.zoneId,
        name: option.resourceRecordName,
        type: option.resourceRecordType,
        records: [option.resourceRecordValue],
        ttl: 60,
      }),
  ),
)
const certificateValidation = new aws.acm.CertificateValidation('certificate', {
  certificateArn: certificate.arn,
  validationRecordFqdns: validations.apply((records) => records.map((record) => record.fqdn)),
})
const alb = new aws.lb.LoadBalancer('https', {
  loadBalancerType: 'application',
  securityGroups: [albSg.id],
  subnets: publicSubnets.map((subnet) => subnet.id),
  tags,
})
const httpTarget = new aws.lb.TargetGroup('http', {
  port: 8081,
  protocol: 'HTTP',
  targetType: 'ip',
  vpcId: vpc.id,
  deregistrationDelay: 60,
  healthCheck: { path: '/health/ready', matcher: '200', interval: 30 },
  tags,
})
const httpsListener = new aws.lb.Listener('https', {
  loadBalancerArn: alb.arn,
  port: 443,
  protocol: 'HTTPS',
  certificateArn: certificateValidation.certificateArn,
  defaultActions: [{ type: 'forward', targetGroupArn: httpTarget.arn }],
  tags,
})
const nlb = new aws.lb.LoadBalancer('ssh', {
  loadBalancerType: 'network',
  subnets: publicSubnets.map((subnet) => subnet.id),
  tags,
})
const sshTarget = new aws.lb.TargetGroup('ssh', {
  port: 2222,
  protocol: 'TCP',
  targetType: 'ip',
  vpcId: vpc.id,
  preserveClientIp: 'true',
  healthCheck: { protocol: 'TCP', port: '2222' },
  tags,
})
const sshListener = new aws.lb.Listener('ssh', {
  loadBalancerArn: nlb.arn,
  port: 2222,
  protocol: 'TCP',
  defaultActions: [{ type: 'forward', targetGroupArn: sshTarget.arn }],
  tags,
})
const service = new aws.ecs.Service(
  'service',
  {
    name,
    cluster: cluster.arn,
    taskDefinition: taskDefinition.arn,
    desiredCount: 1,
    enableExecuteCommand: true,
    capacityProviderStrategies: [{ capacityProvider: capacityProvider.name, weight: 1, base: 1 }],
    networkConfiguration: {
      subnets: privateSubnets.map((subnet) => subnet.id),
      securityGroups: [taskSg.id],
    },
    loadBalancers: [
      { targetGroupArn: httpTarget.arn, containerName: 'gateway', containerPort: 8081 },
      { targetGroupArn: sshTarget.arn, containerName: 'adenosine', containerPort: 2222 },
    ],
    deploymentMinimumHealthyPercent: 0,
    deploymentMaximumPercent: 100,
    healthCheckGracePeriodSeconds: 120,
    waitForSteadyState: true,
    tags,
  },
  { dependsOn: [capacityProvider, certificateValidation, backupSelection] },
)
const webRecord = new aws.route53.Record('web', {
  zoneId: cfg.zoneId,
  name: cfg.domain,
  type: 'A',
  aliases: [{ name: alb.dnsName, zoneId: alb.zoneId, evaluateTargetHealth: true }],
})
const sshRecord = new aws.route53.Record('ssh', {
  zoneId: cfg.zoneId,
  name: `ssh.${cfg.domain}`,
  type: 'A',
  aliases: [{ name: nlb.dnsName, zoneId: nlb.zoneId, evaluateTargetHealth: true }],
})

void ecsInstancePolicy
void ssmInstancePolicy
void clusterCapacity
void taskExecutionPolicy
void taskEfsPolicy
void taskExecChannelPolicy
void oauthStateValue
void oauthCredentialValue
void databaseUrlValue
void electricSecretVersion
void tapPasswordVersion
void taskSecretPolicy
void backupPolicy
void httpsListener
void sshListener
void webRecord
void sshRecord

export const clusterName = cluster.name
export const serviceName = service.name
export const taskDefinitionArn = taskDefinition.arn
export const webUrl = `https://${cfg.domain}`
export const gitHttpsBase = `https://${cfg.domain}`
export const sshHost = `ssh.${cfg.domain}`
export const sshPort = 2222
export const apiDocsUrl = `https://${cfg.domain}/docs/api`
export const healthUrl = `https://${cfg.domain}/health/ready`
export const image = cfg.image
export const databaseIdentifier = database.identifier
export const repositoryFileSystemId = filesystem.id
