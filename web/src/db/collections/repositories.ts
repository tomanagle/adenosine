import { zSyncRepository } from '@adenosine/api-client/schemas'
import { FetchError } from '@electric-sql/client'
import { BTreeIndex, createCollection } from '@tanstack/db'
import { electricCollectionOptions } from '@tanstack/electric-db-collection'

export function createRepositoryCollection() {
  if (typeof window === 'undefined') {
    throw new Error('Repository live collections are browser-only')
  }

  const collection = createCollection(
    electricCollectionOptions({
      id: 'home-repositories',
      schema: zSyncRepository,
      getKey: (repository) => repository.uri,
      syncMode: 'on-demand',
      shapeOptions: {
        url: new URL('/api/v1/sync/repositories', window.location.origin).toString(),
        subsetMethod: 'POST',
        onError: (error) => {
          if (error instanceof FetchError && error.status >= 400 && error.status < 500 && error.status !== 429) return
          return {}
        },
      },
    }),
  )
  collection.createIndex((repository) => repository.indexed_at, { indexType: BTreeIndex })
  return collection
}

export type RepositoryCollection = ReturnType<typeof createRepositoryCollection>
