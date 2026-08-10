import { Radio, WifiOff } from 'lucide-react'
import { eq, useLiveQuery } from '@tanstack/react-db'

import { Badge } from '@/components/ui/badge'
import type { RouteElectricCollection } from '@/db/collections/route-electric'
import { useRouteElectricCollections } from '@/db/collections/route-lifecycle'

const HOME_LIVE_RESOURCES = ['repositories', 'profiles'] as const

export function LiveRepositories() {
  const { collections, error } = useRouteElectricCollections('index', HOME_LIVE_RESOURCES)

  if (error) return <LiveUnavailable />
  if (!collections) return <LiveConnecting />

  return <LiveRepositoryQuery repositories={collections.repositories} profiles={collections.profiles} />
}

function LiveRepositoryQuery({
  repositories,
  profiles,
}: {
  repositories: RouteElectricCollection<'repositories'>
  profiles: RouteElectricCollection<'profiles'>
}) {
  const live = useLiveQuery((query) =>
    query
      .from({ repository: repositories })
      .leftJoin({ owner: profiles }, ({ repository, owner }) => eq(repository.owner_did, owner.did))
      .orderBy(({ repository }) => repository.indexed_at, 'desc')
      .limit(5)
      .select(({ repository, owner }) => ({
        uri: repository.uri,
        ownerDid: repository.owner_did,
        ownerHandle: owner?.handle,
        name: repository.name,
        slug: repository.slug,
        starCount: repository.star_count,
      })),
  )

  if (live.isError) return <LiveUnavailable />

  if (!live.isReady) return <LiveConnecting />

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
                <p className="truncate text-xs text-muted-foreground">{repository.ownerHandle ?? repository.ownerDid}</p>
              </div>
              <span className="text-xs tabular-nums text-muted-foreground">{repository.starCount.toString()} stars</span>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  )
}

function LiveConnecting() {
  return (
    <div className="flex items-center gap-2 text-sm text-muted-foreground">
      <Radio className="size-4" aria-hidden="true" />
      Connecting live repository updates...
    </div>
  )
}

function LiveUnavailable() {
  return (
    <div className="flex items-center gap-2 text-sm text-muted-foreground">
      <WifiOff className="size-4" aria-hidden="true" />
      Live updates unavailable. The REST snapshot remains available.
    </div>
  )
}
