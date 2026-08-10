import type { CurrentIdentity } from '@adenosine/api-client'
import { getCurrentIdentityOptions } from '@adenosine/api-client/query'
import { queryOptions } from '@tanstack/react-query'

import { browserApiClient } from '@/api/browser-client'
import { getIdentity } from './identity.functions'

export const identityQueryOptions = () => {
  const generated = getCurrentIdentityOptions({ client: browserApiClient })

  return queryOptions<CurrentIdentity | null>({
    queryKey: generated.queryKey,
    queryFn: () => getIdentity(),
    retry: false,
    staleTime: 30_000,
  })
}
