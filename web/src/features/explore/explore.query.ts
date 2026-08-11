import { searchProfiles, searchRepositories } from '@adenosine/api-client'
import { searchProfilesOptions, searchRepositoriesOptions } from '@adenosine/api-client/query'
import { queryOptions } from '@tanstack/react-query'

import { browserApiClient } from '@/api/browser-client'

import type { ExploreSearch } from './explore-search'

export function repositorySearchQueryOptions(search: ExploreSearch) {
  const options = {
    client: browserApiClient,
    query: { q: search.q, sort: search.sort, limit: 20, cursor: search.cursor },
  } as const
  const generated = searchRepositoriesOptions(options)
  return queryOptions({
    queryKey: generated.queryKey,
    queryFn: async ({ signal }) => {
      const result = await searchRepositories({ ...options, signal })
      if (!result.data) throw new Error('Repository search is temporarily unavailable.')
      return result.data
    },
    staleTime: 30_000,
  })
}

export function profileSearchQueryOptions(search: ExploreSearch) {
  const options = {
    client: browserApiClient,
    query: { q: search.q, sort: search.sort, limit: 20, cursor: search.cursor },
  } as const
  const generated = searchProfilesOptions(options)
  return queryOptions({
    queryKey: generated.queryKey,
    queryFn: async ({ signal }) => {
      const result = await searchProfiles({ ...options, signal })
      if (!result.data) throw new Error('Profile search is temporarily unavailable.')
      return result.data
    },
    staleTime: 30_000,
  })
}
