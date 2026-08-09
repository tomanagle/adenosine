import { useEffect, useState } from 'react'
import { Radio, WifiOff } from 'lucide-react'
import { useLiveQuery } from '@tanstack/react-db'

import { Badge } from '@/components/ui/badge'
import { createRepositoryCollection, type RepositoryCollection } from '@/db/collections/repositories'

export function LiveRepositories() {
  const [collection] = useState<RepositoryCollection>(createRepositoryCollection)
  const live = useLiveQuery((query) =>
    query
      .from({ repository: collection })
      .orderBy(({ repository }) => repository.indexed_at, 'desc')
      .limit(5)
      .select(({ repository }) => ({
        uri: repository.uri,
        ownerDid: repository.owner_did,
        name: repository.name,
        slug: repository.slug,
        starCount: repository.star_count,
      })),
  )

  useEffect(() => {
    return () => {
      void collection.cleanup()
    }
  }, [collection])

  if (live.isError) {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <WifiOff className="size-4" aria-hidden="true" />
        Live updates unavailable. The REST snapshot remains available.
      </div>
    )
  }

  if (!live.isReady) {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Radio className="size-4" aria-hidden="true" />
        Connecting live repository updates...
      </div>
    )
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Radio className="size-4" aria-hidden="true" />
        Live updates connected
        <Badge variant="outline">{live.data?.length ?? 0} synced</Badge>
      </div>
      {(live.data?.length ?? 0) > 0 ? (
        <ul className="divide-y rounded-lg border" aria-label="Live repositories">
          {live.data?.slice(0, 5).map((repository) => (
            <li className="flex items-center justify-between gap-4 p-3" key={repository.uri}>
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">{repository.name ?? repository.slug ?? repository.uri}</p>
                <p className="truncate text-xs text-muted-foreground">{repository.ownerDid}</p>
              </div>
              <span className="text-xs tabular-nums text-muted-foreground">{repository.starCount.toString()} stars</span>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  )
}
