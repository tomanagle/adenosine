import * as pulumi from '@pulumi/pulumi'

export interface AwsConfig {
  domain: string
  image: string
  zoneId: string
  instanceType: string
  databaseInstanceClass: string
  databaseAllocatedStorage: number
  backupRetentionDays: number
  repositoryPerformanceMode: 'generalPurpose' | 'maxIO'
  sshAllowedCidrs: string[]
  enableElectric: boolean
  enableTap: boolean
  oauthStateKey: pulumi.Output<string>
  oauthCredentialKey: pulumi.Output<string>
}

export function loadConfig(config = new pulumi.Config()): AwsConfig {
  const domain = config.require('domain').trim().toLowerCase()
  const image = config.require('image').trim()
  const zoneId = config.require('zoneId').trim()
  const backupRetentionDays = config.getNumber('backupRetentionDays') ?? 14
  const databaseAllocatedStorage = config.getNumber('databaseAllocatedStorage') ?? 50
  const repositoryPerformanceMode = config.get('repositoryPerformanceMode') ?? 'generalPurpose'
  const sshAllowedCidrs = config.getObject<string[]>('sshAllowedCidrs') ?? ['0.0.0.0/0']

  if (!/^[a-z0-9.-]+\.[a-z]{2,}$/i.test(domain))
    throw new pulumi.RunError('domain must be a DNS hostname')
  if (!/^\S+@sha256:[a-f0-9]{64}$/.test(image))
    throw new pulumi.RunError('image must be an explicit immutable OCI digest')
  if (!zoneId) throw new pulumi.RunError('zoneId must identify the Route53 hosted zone for domain')
  if (!Number.isInteger(backupRetentionDays) || backupRetentionDays < 1 || backupRetentionDays > 35)
    throw new pulumi.RunError('backupRetentionDays must be from 1 through 35')
  if (!Number.isInteger(databaseAllocatedStorage) || databaseAllocatedStorage < 20)
    throw new pulumi.RunError('databaseAllocatedStorage must be at least 20 GiB')
  if (repositoryPerformanceMode !== 'generalPurpose' && repositoryPerformanceMode !== 'maxIO')
    throw new pulumi.RunError('repositoryPerformanceMode must be generalPurpose or maxIO')
  if (
    !sshAllowedCidrs.length ||
    sshAllowedCidrs.some((cidr) => !/^([0-9a-f:.]+)\/\d+$/i.test(cidr))
  )
    throw new pulumi.RunError('sshAllowedCidrs must contain CIDR blocks')

  return {
    domain,
    image,
    zoneId,
    backupRetentionDays,
    databaseAllocatedStorage,
    repositoryPerformanceMode: repositoryPerformanceMode as AwsConfig['repositoryPerformanceMode'],
    sshAllowedCidrs,
    instanceType: config.get('instanceType') ?? 't3.xlarge',
    databaseInstanceClass: config.get('databaseInstanceClass') ?? 'db.t4g.medium',
    enableElectric: config.getBoolean('enableElectric') ?? true,
    enableTap: config.getBoolean('enableTap') ?? true,
    oauthStateKey: config.requireSecret('oauthStateKey'),
    oauthCredentialKey: config.requireSecret('oauthCredentialKey'),
  }
}
