import * as pulumi from '@pulumi/pulumi'

export interface RailwayConfig {
  domain: string
  image: string
  region: string
  workspaceId?: string
  enableElectric: boolean
  enableTap: boolean
  backupRetentionDays: number
  oauthStateKey: pulumi.Output<string>
  oauthCredentialKey: pulumi.Output<string>
}

export function loadConfig(config = new pulumi.Config()): RailwayConfig {
  const domain = config.require('domain').trim().toLowerCase()
  const image = config.require('image').trim()
  const region = config.get('region')?.trim() || 'us-west1'
  const backupRetentionDays = config.getNumber('backupRetentionDays') ?? 7

  if (!/^[a-z0-9.-]+\.[a-z]{2,}$/i.test(domain)) {
    throw new pulumi.RunError('domain must be a DNS hostname without a scheme or path')
  }
  if (!/^\S+@sha256:[a-f0-9]{64}$/.test(image)) {
    throw new pulumi.RunError('image must be an explicit immutable OCI digest (name@sha256:...)')
  }
  if (!/^[a-z]{2,}-[a-z]+\d$/.test(region)) {
    throw new pulumi.RunError('region must be a Railway region such as us-west1')
  }
  if (
    !Number.isInteger(backupRetentionDays) ||
    backupRetentionDays < 1 ||
    backupRetentionDays > 90
  ) {
    throw new pulumi.RunError('backupRetentionDays must be an integer from 1 through 90')
  }

  return {
    domain,
    image,
    region,
    workspaceId: config.get('workspaceId')?.trim() || undefined,
    enableElectric: config.getBoolean('enableElectric') ?? true,
    enableTap: config.getBoolean('enableTap') ?? true,
    backupRetentionDays,
    oauthStateKey: config.requireSecret('oauthStateKey'),
    oauthCredentialKey: config.requireSecret('oauthCredentialKey'),
  }
}
