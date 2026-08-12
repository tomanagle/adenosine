import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import * as pulumi from '@pulumi/pulumi'

interface RegisteredResource {
  name: string
  inputs: Record<string, unknown>
}

const resources: RegisteredResource[] = []
const program = pulumi.runtime
  .setMocks(
    {
      newResource(args) {
        resources.push({ name: args.name, inputs: args.inputs })
        return { id: `${args.name}-id`, state: args.inputs }
      },
      call(args) {
        return args.inputs
      },
    },
    'adenosine-railway',
    'test',
  )
  .then(() =>
    pulumi.runtime.runInPulumiStack(async () => {
      const { Deployment, Domain, Service, Volume } = await import('./railway')
      const app = new Service('app', {
        projectId: 'project-id',
        environmentId: 'environment-id',
        name: 'adenosine',
        image: `ghcr.io/example/adenosine@sha256:${'a'.repeat(64)}`,
        region: 'us-west1',
        variables: { RAILWAY_RUN_UID: '0' },
        preDeployCommand: ['adenosine migrate'],
        startCommand: 'initialize; exec su adenosine -c "adenosine serve"',
      })
      const volume = new Volume('adenosine-data', {
        projectId: 'project-id',
        environmentId: 'environment-id',
        serviceId: app.id,
        mountPath: '/var/lib/adenosine',
        region: 'us-west1',
      })
      const deployment = new Deployment(
        'app-deployment',
        {
          serviceId: app.id,
          environmentId: 'environment-id',
          revision: `ghcr.io/example/adenosine@sha256:${'a'.repeat(64)}`,
        },
        { dependsOn: volume },
      )
      const domain = new Domain('web-domain', {
        projectId: 'project-id',
        environmentId: 'environment-id',
        serviceId: 'gateway-id',
        domain: 'forge.example.com',
        targetPort: 8080,
      })
      return { deployment, domain }
    }),
  )

function named(name: string): RegisteredResource {
  const value = resources.find((candidate) => candidate.name === name)
  if (!value) throw new Error(`resource not registered: ${name}`)
  return value
}

describe('Railway provider resource contract', () => {
  test('places image source only on service creation', async () => {
    await program
    const { serviceCreateInput, serviceInstanceUpdateInput } = await import('./railway')
    const inputs = {
      projectId: 'project-id',
      name: 'service',
      image: `ghcr.io/example/app@sha256:${'b'.repeat(64)}`,
      region: 'us-west1',
    }
    assert.deepEqual(serviceCreateInput(inputs).source, { image: inputs.image })
    assert.equal(serviceInstanceUpdateInput(inputs).source, undefined)
  })

  test('keeps provider-specific fields out of custom domains', async () => {
    await program
    const domain = named('web-domain')
    assert.equal(domain.inputs.serviceId, 'gateway-id')
    assert.equal(domain.inputs.region, undefined)
    assert.equal(domain.inputs.targetPort, 8080)
  })

  test('models volume attachment and immutable deployment separately', async () => {
    await program
    assert.equal(named('adenosine-data').inputs.mountPath, '/var/lib/adenosine')
    assert.match(named('app-deployment').inputs.revision as string, /@sha256:/)
  })

  test('runs migration before serving and drops privileges after initialization', async () => {
    await program
    const app = named('app')
    assert.deepEqual(app.inputs.preDeployCommand, ['adenosine migrate'])
    assert.match(app.inputs.startCommand as string, /su adenosine/)
  })

  test('preserves Postgres initialization and invokes the production web executable', async () => {
    await program
    const { postgresStartCommand, webStartCommand } = await import('./railway')
    assert.match(postgresStartCommand, /^docker-entrypoint\.sh postgres /)
    assert.equal(webStartCommand, 'bun /opt/adenosine/web/server/index.mjs')
  })

  test('builds Electric URL with the provisioned role', async () => {
    await program
    const { electricDatabaseUrl } = await import('./railway')
    const value = await new Promise<string>((resolve) =>
      electricDatabaseUrl('electric-password').apply(resolve),
    )
    assert.equal(
      value,
      'postgresql://electric:electric-password@postgres.railway.internal:5432/adenosine',
    )
  })
})
