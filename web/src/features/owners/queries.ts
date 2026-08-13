import { getOwnerOptions } from '@adenosine/api-client/query'

import { browserApiClient } from '@/api/browser-client'

export const ownerQueryOptions = (owner: string) =>
  getOwnerOptions({ client: browserApiClient, path: { owner } })
