import { listNetworkRepositories } from '@adenosine/api-client'
import type { Repository } from '@adenosine/api-client'
import { listNetworkRepositoriesOptions } from '@adenosine/api-client/query'
import { queryOptions } from '@tanstack/react-query'

import { browserApiClient } from '@/api/browser-client'

export type RepositorySnapshot = {
  repositories: Repository[]
  available: boolean
}

export const repositorySnapshotQueryOptions = () => {
  const generated = listNetworkRepositoriesOptions({ client: browserApiClient })

  return queryOptions<RepositorySnapshot>({
    queryKey: generated.queryKey,
    queryFn: async ({ signal }): Promise<RepositorySnapshot> => {
      const result = await listNetworkRepositories({ client: browserApiClient, signal })
      if (!result.data) return { repositories: [], available: false }
      return { repositories: result.data.data, available: true }
    },
    staleTime: 30_000,
  })
}
