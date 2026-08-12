import { getDeveloperProfileOptions } from '@adenosine/api-client/query'

import { browserApiClient } from '@/api/browser-client'

export const profileQueryOptions = (did: string) =>
  getDeveloperProfileOptions({ client: browserApiClient, path: { did } })
