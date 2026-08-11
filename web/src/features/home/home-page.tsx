import { GitFork, Star } from 'lucide-react'
import { useSuspenseQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { identityQueryOptions } from '@/features/identity/identity.query'
import { LiveRepositories } from '@/features/repositories/live-repositories'
import { repositorySnapshotQueryOptions } from '@/features/repositories/repository-snapshot.query'

export function HomePage() {
  const { data: identity } = useSuspenseQuery(identityQueryOptions())
  const { data: snapshot } = useSuspenseQuery(repositorySnapshotQueryOptions())

  return (
    <main className="min-h-screen bg-muted/30">
      <header className="border-b bg-background">
        <div className="mx-auto flex min-h-16 max-w-6xl items-center justify-between gap-4 px-5 py-3 sm:px-8">
          <div className="font-semibold tracking-tight">Adenosine</div>
          <div className="flex min-w-0 items-center gap-4 text-right">
            <Link
              to="/explore"
              search={{ q: '', type: 'repositories', sort: 'relevance' }}
              className="text-sm font-medium underline-offset-4 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              Explore
            </Link>
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{identity?.handle ?? 'Signed in'}</p>
              <p className="truncate text-xs text-muted-foreground">{identity?.did}</p>
            </div>
          </div>
        </div>
      </header>
      <div className="mx-auto max-w-6xl space-y-6 px-5 py-8 sm:px-8 sm:py-12">
        <div>
          <p className="text-sm text-muted-foreground">Personal home</p>
          <h1 className="mt-1 font-serif text-3xl tracking-tight sm:text-4xl">
            Welcome back{identity?.handle ? `, ${identity.handle.split('.')[0]}` : ''}.
          </h1>
          <p className="mt-2 text-muted-foreground">
            Your identity, a public repository snapshot, and live network projection in one place.
          </p>
        </div>
        <div className="grid gap-6 lg:grid-cols-[1.2fr_0.8fr]">
          <Card>
            <CardHeader>
              <div className="flex items-center justify-between gap-4">
                <CardTitle>Repository snapshot</CardTitle>
                <Badge variant="secondary">REST</Badge>
              </div>
              <CardDescription>
                The public network list is the durable fallback while personal repository listing is
                not yet available.
              </CardDescription>
            </CardHeader>
            <CardContent>
              {!snapshot.available ? (
                <Alert>
                  <AlertTitle>Snapshot temporarily unavailable</AlertTitle>
                  <AlertDescription>
                    Your identity is loaded. Repository data can be retried without relying on live
                    sync.
                  </AlertDescription>
                </Alert>
              ) : snapshot.repositories.length === 0 ? (
                <p className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">
                  No public repositories are visible on this network yet.
                </p>
              ) : (
                <ul className="divide-y rounded-lg border">
                  {snapshot.repositories.slice(0, 6).map((repository) => (
                    <li
                      className="p-4"
                      key={
                        repository.uri ??
                        repository.id ??
                        `${repository.owner.did}/${repository.slug}`
                      }
                    >
                      <div className="flex items-start justify-between gap-4">
                        <div className="min-w-0">
                          <p className="truncate font-medium">
                            {repository.owner.handle ?? repository.owner.did} / {repository.slug}
                          </p>
                          <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">
                            {repository.description ?? 'No description provided.'}
                          </p>
                        </div>
                        <Badge variant="outline">
                          {repository.hosting.local ? 'Hosted here' : 'Remote'}
                        </Badge>
                      </div>
                      <div className="mt-3 flex gap-4 text-xs text-muted-foreground">
                        <span className="flex items-center gap-1">
                          <Star className="size-3.5" />
                          {repository.star_count}
                        </span>
                        <span className="flex items-center gap-1">
                          <GitFork className="size-3.5" />
                          {repository.default_branch}
                        </span>
                      </div>
                    </li>
                  ))}
                </ul>
              )}
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <div className="flex items-center justify-between gap-4">
                <CardTitle>Network projection</CardTitle>
                <Badge variant="secondary">Electric</Badge>
              </div>
              <CardDescription>
                Route-scoped repository rows update without replacing the REST path.
              </CardDescription>
            </CardHeader>
            <CardContent>
              <LiveRepositories />
            </CardContent>
          </Card>
        </div>
      </div>
    </main>
  )
}
