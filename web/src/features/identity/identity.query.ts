import { getCurrentIdentity } from '@adenosine/api-client'
import type { CurrentIdentity } from '@adenosine/api-client'
import { getCurrentIdentityOptions } from '@adenosine/api-client/query'
import { queryOptions } from '@tanstack/react-query'

import { browserApiClient } from '@/api/browser-client'
import { classifyIdentityResult } from './identity'

export const identityQueryOptions = () => {
  const generated = getCurrentIdentityOptions({ client: browserApiClient })

  return queryOptions<CurrentIdentity | null>({
    queryKey: generated.queryKey,
    queryFn: async ({ signal }) => {
      const result = await getCurrentIdentity({ client: browserApiClient, signal })
      return classifyIdentityResult(result)
    },
    retry: false,
    staleTime: 30_000,
  })
}
