import { zSyncRepository } from '@adenosine/api-client/schemas'
import { FetchError } from '@electric-sql/client'
import { createCollection } from '@tanstack/db'
import { electricCollectionOptions } from '@tanstack/electric-db-collection'

export function createRepositoryCollection() {
  if (typeof window === 'undefined') {
    throw new Error('Repository live collections are browser-only')
  }

  return createCollection(
    electricCollectionOptions({
      id: 'home-repositories',
      schema: zSyncRepository,
      getKey: (repository) => repository.uri,
      syncMode: 'on-demand',
      shapeOptions: {
        url: '/api/v1/sync/repositories',
        subsetMethod: 'POST',
        onError: (error) => {
          if (error instanceof FetchError && error.status >= 400 && error.status < 500 && error.status !== 429) return
          return {}
        },
      },
    }),
  )
}

export type RepositoryCollection = ReturnType<typeof createRepositoryCollection>
