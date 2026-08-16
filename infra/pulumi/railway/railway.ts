import * as pulumi from '@pulumi/pulumi'

const endpoint = 'https://backboard.railway.com/graphql/v2'

type Inputs = pulumi.Inputs
type GraphQLResponse<T> = { data?: T; errors?: Array<{ message: string }> }

export function serviceCreateInput(inputs: Inputs) {
  return {
    projectId: inputs.projectId,
    name: inputs.name,
    source: { image: inputs.image },
  }
}

export function serviceInstanceUpdateInput(inputs: Inputs) {
  return {
    region: inputs.region,
    healthcheckPath: inputs.healthcheckPath,
    startCommand: inputs.startCommand,
    preDeployCommand: inputs.preDeployCommand,
    restartPolicyType: 'ALWAYS',
    numReplicas: 1,
  }
}

export const postgresStartCommand =
  'docker-entrypoint.sh postgres -c wal_level=logical -c max_replication_slots=5 -c max_wal_senders=5 -c max_slot_wal_keep_size=1GB'
export const webStartCommand = 'bun /opt/adenosine/web/server/index.mjs'

export function electricDatabaseUrl(
  password: pulumi.Input<string>,
  host = 'postgres.railway.internal',
): pulumi.Output<string> {
  return pulumi.interpolate`postgresql://electric:${password}@${host}:5432/adenosine`
}

async function graphql<T>(query: string, variables: Inputs): Promise<T> {
  const token = process.env.RAILWAY_API_TOKEN
  if (!token) throw new Error('RAILWAY_API_TOKEN is required')
  const response = await fetch(endpoint, {
    method: 'POST',
    headers: { authorization: `Bearer ${token}`, 'content-type': 'application/json' },
    body: JSON.stringify({ query, variables }),
  })
  const result: GraphQLResponse<T> = await response.json()
  if (!response.ok || result.errors?.length || !result.data) {
    throw new Error(
      `Railway API request failed: ${result.errors?.map((error) => error.message).join('; ') || response.status}`,
    )
  }
  return result.data
}

export interface ProjectArgs {
  name: string
  workspaceId?: string
}
interface ProjectState extends ProjectArgs {
  environmentId?: string
}

class ProjectProvider implements pulumi.dynamic.ResourceProvider {
  async create(inputs: ProjectArgs): Promise<pulumi.dynamic.CreateResult> {
    const result = await graphql<{ projectCreate: { id: string } }>(
      'mutation($input: ProjectCreateInput!) { projectCreate(input: $input) { id } }',
      {
        input: {
          name: inputs.name,
          workspaceId: inputs.workspaceId,
          defaultEnvironmentName: 'production',
        },
      },
    )
    const project = await graphql<{
      project: { environments: { edges: Array<{ node: { id: string; name: string } }> } }
    }>('query($id: String!) { project(id: $id) { environments { edges { node { id name } } } } }', {
      id: result.projectCreate.id,
    })
    const environmentId = project.project.environments.edges.find(
      ({ node }) => node.name === 'production',
    )?.node.id
    if (!environmentId) throw new Error('Railway did not create the production environment')
    return { id: result.projectCreate.id, outs: { ...inputs, environmentId } }
  }
  async update(
    id: string,
    _olds: ProjectState,
    news: ProjectArgs,
  ): Promise<pulumi.dynamic.UpdateResult> {
    await graphql(
      'mutation($id: String!, $input: ProjectUpdateInput!) { projectUpdate(id: $id, input: $input) { id } }',
      { id, input: { name: news.name } },
    )
    return { outs: { ...news, environmentId: _olds.environmentId } }
  }
  async delete(id: string): Promise<void> {
    await graphql('mutation($id: String!) { projectDelete(id: $id) }', { id })
  }
}

export class Project extends pulumi.dynamic.Resource {
  readonly environmentId!: pulumi.Output<string>
  constructor(name: string, args: ProjectArgs, opts?: pulumi.CustomResourceOptions) {
    super(new ProjectProvider(), name, args, opts)
  }
}

export interface ServiceArgs {
  projectId: pulumi.Input<string>
  environmentId: pulumi.Input<string>
  name: string
  image: string
  region: string
  variables: pulumi.Input<Record<string, pulumi.Input<string>>>
  startCommand?: string
  preDeployCommand?: string[]
  healthcheckPath?: string
}

class ServiceProvider implements pulumi.dynamic.ResourceProvider {
  async create(inputs: Inputs): Promise<pulumi.dynamic.CreateResult> {
    const created = await graphql<{ serviceCreate: { id: string } }>(
      'mutation($input: ServiceCreateInput!) { serviceCreate(input: $input) { id } }',
      {
        input: serviceCreateInput(inputs),
      },
    )
    const id = created.serviceCreate.id
    await this.configureService(id, inputs)
    return { id, outs: inputs }
  }
  async update(id: string, _olds: Inputs, news: Inputs): Promise<pulumi.dynamic.UpdateResult> {
    await this.configureService(id, news)
    return { outs: news }
  }
  async diff(_id: string, olds: Inputs, news: Inputs): Promise<pulumi.dynamic.DiffResult> {
    const replaceKeys = ['projectId', 'environmentId'].filter((key) => olds[key] !== news[key])
    return {
      changes: JSON.stringify(olds) !== JSON.stringify(news),
      replaces: replaceKeys,
      deleteBeforeReplace: false,
    }
  }
  async delete(id: string): Promise<void> {
    await graphql('mutation($id: String!) { serviceDelete(id: $id) }', { id })
  }
  private async configureService(id: string, inputs: Inputs): Promise<void> {
    await graphql(
      'mutation($serviceId: String!, $environmentId: String!, $input: ServiceInstanceUpdateInput!) { serviceInstanceUpdate(serviceId: $serviceId, environmentId: $environmentId, input: $input) }',
      {
        serviceId: id,
        environmentId: inputs.environmentId,
        input: serviceInstanceUpdateInput(inputs),
      },
    )
    await graphql(
      'mutation($input: VariableCollectionUpsertInput!) { variableCollectionUpsert(input: $input) }',
      {
        input: {
          projectId: inputs.projectId,
          environmentId: inputs.environmentId,
          serviceId: id,
          variables: inputs.variables,
          replace: true,
          skipDeploys: true,
        },
      },
    )
  }
}

export class Service extends pulumi.dynamic.Resource {
  constructor(name: string, args: ServiceArgs, opts?: pulumi.CustomResourceOptions) {
    super(new ServiceProvider(), name, args, opts)
  }
}

export interface DeploymentArgs {
  serviceId: pulumi.Input<string>
  environmentId: pulumi.Input<string>
  revision: pulumi.Input<string>
}
class DeploymentProvider implements pulumi.dynamic.ResourceProvider {
  async create(inputs: Inputs): Promise<pulumi.dynamic.CreateResult> {
    const result = await graphql<{ serviceInstanceDeployV2: string }>(
      'mutation($serviceId: String!, $environmentId: String!) { serviceInstanceDeployV2(serviceId: $serviceId, environmentId: $environmentId) }',
      { serviceId: inputs.serviceId, environmentId: inputs.environmentId },
    )
    return { id: result.serviceInstanceDeployV2, outs: inputs }
  }
  async diff(_id: string, olds: Inputs, news: Inputs): Promise<pulumi.dynamic.DiffResult> {
    return {
      changes: JSON.stringify(olds) !== JSON.stringify(news),
      replaces: ['serviceId', 'environmentId', 'revision'].filter((key) => olds[key] !== news[key]),
      deleteBeforeReplace: false,
    }
  }
}
export class Deployment extends pulumi.dynamic.Resource {
  constructor(name: string, args: DeploymentArgs, opts?: pulumi.CustomResourceOptions) {
    super(new DeploymentProvider(), name, args, opts)
  }
}

export interface VolumeArgs {
  projectId: pulumi.Input<string>
  environmentId: pulumi.Input<string>
  serviceId: pulumi.Input<string>
  mountPath: string
  region: string
}
class VolumeProvider implements pulumi.dynamic.ResourceProvider {
  async create(inputs: Inputs): Promise<pulumi.dynamic.CreateResult> {
    const result = await graphql<{ volumeCreate: { id: string } }>(
      'mutation($input: VolumeCreateInput!) { volumeCreate(input: $input) { id } }',
      { input: inputs },
    )
    return { id: result.volumeCreate.id, outs: inputs }
  }
  async diff(_id: string, olds: Inputs, news: Inputs): Promise<pulumi.dynamic.DiffResult> {
    const keys = ['projectId', 'environmentId', 'serviceId', 'mountPath', 'region']
    return {
      changes: keys.some((key) => olds[key] !== news[key]),
      replaces: keys.filter((key) => olds[key] !== news[key]),
      deleteBeforeReplace: false,
    }
  }
  async delete(id: string): Promise<void> {
    await graphql('mutation($volumeId: String!) { volumeDelete(volumeId: $volumeId) }', {
      volumeId: id,
    })
  }
}
export class Volume extends pulumi.dynamic.Resource {
  constructor(name: string, args: VolumeArgs, opts?: pulumi.CustomResourceOptions) {
    super(new VolumeProvider(), name, args, opts)
  }
}

export interface DomainArgs {
  projectId: pulumi.Input<string>
  environmentId: pulumi.Input<string>
  serviceId: pulumi.Input<string>
  domain: string
  targetPort: number
}
class DomainProvider implements pulumi.dynamic.ResourceProvider {
  async create(inputs: Inputs): Promise<pulumi.dynamic.CreateResult> {
    const result = await graphql<{ customDomainCreate: { id: string } }>(
      'mutation($input: CustomDomainCreateInput!) { customDomainCreate(input: $input) { id } }',
      { input: inputs },
    )
    return { id: result.customDomainCreate.id, outs: inputs }
  }
  async diff(_id: string, olds: Inputs, news: Inputs): Promise<pulumi.dynamic.DiffResult> {
    return {
      changes: JSON.stringify(olds) !== JSON.stringify(news),
      replaces: ['projectId', 'environmentId', 'serviceId', 'domain'],
    }
  }
  async delete(id: string): Promise<void> {
    await graphql('mutation($id: String!) { customDomainDelete(id: $id) }', { id })
  }
}
export class Domain extends pulumi.dynamic.Resource {
  constructor(name: string, args: DomainArgs, opts?: pulumi.CustomResourceOptions) {
    super(new DomainProvider(), name, args, opts)
  }
}
