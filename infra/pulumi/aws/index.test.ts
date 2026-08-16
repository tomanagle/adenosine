import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import * as pulumi from '@pulumi/pulumi'
import { z } from 'zod'

interface RegisteredResource {
  type: string
  name: string
  id: string
  inputs: pulumi.Inputs
}

const containerDefinitionsSchema = z.array(
  z.object({
    name: z.string(),
    cpu: z.number().optional(),
    memory: z.number().optional(),
    dependsOn: z.array(z.object({ containerName: z.string(), condition: z.string() })).optional(),
    portMappings: z
      .array(z.object({ containerPort: z.number(), protocol: z.string().optional() }))
      .optional(),
    secrets: z.array(z.object({ name: z.string(), valueFrom: z.string() })).optional(),
  }),
)

const resources: RegisteredResource[] = []
const program = pulumi.runtime
  .setMocks(
    {
      newResource(args) {
        const id = `${args.name}-id`
        resources.push({ type: args.type, name: args.name, id, inputs: args.inputs })
        const state = { ...args.inputs }
        if (args.type === 'aws:secretsmanager/secretVersion:SecretVersion')
          state.arn = `arn:aws:secretsmanager:us-east-1:123456789012:secret:${args.name}`
        if (args.type === 'aws:acm/certificate:Certificate')
          state.domainValidationOptions = [
            {
              resourceRecordName: '_validation.forge.example.com',
              resourceRecordType: 'CNAME',
              resourceRecordValue: 'validation.example.com',
            },
          ]
        return { id, state }
      },
      call(args) {
        if (args.token === 'aws:index/getAvailabilityZones:getAvailabilityZones')
          return { names: ['us-east-1a', 'us-east-1b'] }
        if (args.token === 'aws:ssm/getParameter:getParameter') return { value: 'ami-12345678' }
        if (args.token === 'aws:index/getRegion:getRegion') return { name: 'us-east-1' }
        return args.inputs
      },
    },
    'adenosine-aws',
    'test',
  )
  .then(() =>
    pulumi.runtime.setAllConfig(
      {
        'adenosine-aws:domain': 'forge.example.com',
        'adenosine-aws:image': `ghcr.io/example/adenosine@sha256:${'a'.repeat(64)}`,
        'adenosine-aws:zoneId': 'Z123456789',
        'adenosine-aws:oauthStateKey': 'MDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDA=',
        'adenosine-aws:oauthCredentialKey': 'MTExMTExMTExMTExMTExMTExMTExMTExMTExMTExMTE=',
      },
      ['adenosine-aws:oauthStateKey', 'adenosine-aws:oauthCredentialKey'],
    ),
  )
  .then(() => pulumi.runtime.runInPulumiStack(async () => import('./index')))

function resource(type: string, name: string): RegisteredResource {
  const value = resources.find((candidate) => candidate.type === type && candidate.name === name)
  if (!value) throw new Error(`resource not registered: ${type} ${name}`)
  return value
}

describe('AWS resource graph', () => {
  test('protects data and provisions logical replication backups', async () => {
    await program
    assert.equal(
      resource('aws:rds/parameterGroup:ParameterGroup', 'database-parameters').inputs.family,
      'postgres17',
    )
    assert.equal(
      z.array(z.json()).parse(resource('aws:backup/plan:Plan', 'backup-plan').inputs.rules).length,
      1,
    )
    assert.equal(
      z
        .array(z.json())
        .parse(resource('aws:backup/selection:Selection', 'backup-selection').inputs.resources)
        .length,
      2,
    )
  })

  test('routes public HTTP through gateway after migration', async () => {
    await program
    const task = resource('aws:ecs/taskDefinition:TaskDefinition', 'task')
    const definitions = containerDefinitionsSchema.parse(
      JSON.parse(z.string().parse(task.inputs.containerDefinitions)),
    )
    assert.deepEqual(definitions.find(({ name }) => name === 'adenosine')?.dependsOn, [
      {
        containerName: 'migrate',
        condition: 'SUCCESS',
      },
    ])
    assert.deepEqual(definitions.find(({ name }) => name === 'gateway')?.portMappings, [
      {
        containerPort: 8081,
        protocol: 'tcp',
      },
    ])
    assert.equal(resource('aws:lb/targetGroup:TargetGroup', 'http').inputs.port, 8081)
  })

  test('fits the hard task reservation on the default host with headroom', async () => {
    await program
    const launchTemplate = resource('aws:ec2/launchTemplate:LaunchTemplate', 'ecs-host')
    assert.equal(launchTemplate.inputs.instanceType, 't3.xlarge')
    const task = resource('aws:ecs/taskDefinition:TaskDefinition', 'task')
    assert.equal(task.inputs.cpu, '2048')
    assert.equal(task.inputs.memory, '4096')
  })

  test('uses the electric role URL while migration receives its password', async () => {
    await program
    const electricUrl = resource(
      'aws:secretsmanager/secretVersion:SecretVersion',
      'electric-database-url-value',
    )
    assert.match(JSON.stringify(electricUrl.inputs.secretString), /postgresql:\/\/electric:/)
    const electricPassword = resource(
      'aws:secretsmanager/secretVersion:SecretVersion',
      'electric-database-password-value',
    )
    const task = resource('aws:ecs/taskDefinition:TaskDefinition', 'task')
    assert.match(
      z.string().parse(task.inputs.containerDefinitions),
      /secret:electric-database-url-value/,
    )
    assert.match(
      z.string().parse(task.inputs.containerDefinitions),
      /secret:electric-database-password-value/,
    )
    const definitions = containerDefinitionsSchema.parse(
      JSON.parse(z.string().parse(task.inputs.containerDefinitions)),
    )
    assert.ok(definitions.find(({ name }) => name === 'electric'))
    assert.ok(definitions.find(({ name }) => name === 'migrate'))
    assert.equal(electricUrl.id, 'electric-database-url-value-id')
    assert.equal(electricPassword.id, 'electric-database-password-value-id')
  })
})
