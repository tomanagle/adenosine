import { listNetworkRepositories } from '@adenosine/api-client'
import type { Repository } from '@adenosine/api-client'
import { listNetworkRepositoriesOptions } from '@adenosine/api-client/query'
import { queryOptions } from '@tanstack/react-query'

import { browserApiClient } from '@/api/browser-client'

export type RepositorySnapshot = {
  repositories: Repository[]
  available: boolean
}

/**
 * Repository publication is projected asynchronously. Keep the authoritative
 * create response visible until the REST network projection catches up.
 */
export function retainCreatedRepository(
  snapshot: RepositorySnapshot | undefined,
  repository: Repository,
): RepositorySnapshot | undefined {
  if (!snapshot?.available || repository.visibility !== 'public' || repository.state !== 'active') {
    return snapshot
  }

  const alreadyProjected = snapshot.repositories.some(
    (candidate) =>
      (repository.uri && candidate.uri === repository.uri) ||
      (repository.id && candidate.id === repository.id) ||
      (candidate.owner.did === repository.owner.did &&
        candidate.slug.toLowerCase() === repository.slug.toLowerCase()),
  )
  if (alreadyProjected) return snapshot

  return { ...snapshot, repositories: [repository, ...snapshot.repositories] }
}

export const repositorySnapshotQueryOptions = () => {
  const generated = listNetworkRepositoriesOptions({ client: browserApiClient })

  return queryOptions<RepositorySnapshot>({
    queryKey: generated.queryKey,
    queryFn: async ({ signal }): Promise<RepositorySnapshot> => {
      const result = await listNetworkRepositories({ client: browserApiClient, signal })
      if (!result.data) return { repositories: [], available: false }
      return { repositories: result.data.items, available: true }
    },
    staleTime: 30_000,
  })
}
