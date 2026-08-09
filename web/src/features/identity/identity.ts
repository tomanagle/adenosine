import type { CurrentIdentity } from '@adenosine/api-client'
import { zCurrentIdentity } from '@adenosine/api-client/schemas'

type IdentityResult = {
  data?: unknown
  response?: Response
}

export function classifyIdentityResult(result: IdentityResult): CurrentIdentity | null {
  if (result.response?.status === 401) return null
  if (!result.data) throw new Error('Identity request did not return data')
  return zCurrentIdentity.parse(result.data)
}
