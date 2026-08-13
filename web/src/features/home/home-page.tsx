import type { Repository } from '@adenosine/api-client'
import { useSuspenseQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { CircleDot, GitBranch, GitPullRequest, Plus, Search, Star } from 'lucide-react'
import { useState } from 'react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { identityQueryOptions } from '@/features/identity/identity.query'
import { LiveRepositories } from '@/features/repositories/live-repositories'
import { repositorySnapshotQueryOptions } from '@/features/repositories/repository-snapshot.query'
import { cn } from '@/lib/utils'

import { CreatePullRequestPanel } from './create-pull-request-panel'
import { CreateRepositoryPanel } from './create-repository-panel'
import {
  filterRepositories,
  ownedRepositories,
  proposalRepositories,
  repositoryKey,
  pluralize,
  repositoryParams,
  repositorySummary,
  summarySentence,
} from './viewer-repositories'

type Panel = 'repository' | 'pull-request'

export function HomePage() {
  const { data: identity } = useSuspenseQuery(identityQueryOptions())
  const { data: snapshot } = useSuspenseQuery(repositorySnapshotQueryOptions())
  const [panel, setPanel] = useState<Panel>()
  const [filter, setFilter] = useState('')

  const owned = ownedRepositories(snapshot.repositories, identity?.did)
  const proposable = proposalRepositories(owned)
  const summary = repositorySummary(owned)
  const visible = filterRepositories(owned, filter)
  const subtitle = !snapshot.available
    ? 'Repository counts are unavailable until this server can read its projection.'
    : summary.repositories === 0
      ? 'Create a repository to start publishing code from this server.'
      : summarySentence(summary)

  return (
    <div>
      <main className="mx-auto max-w-6xl px-5 py-8 sm:px-8 sm:py-10">
        <div className="flex flex-col gap-5 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <h1 className="font-serif text-3xl tracking-tight sm:text-4xl">
              {identity?.handle ? `Welcome back, ${identity.handle.split('.')[0]}` : 'Welcome back'}
            </h1>
            <p className="mt-2 text-sm text-muted-foreground">{subtitle}</p>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button
              onClick={() => setPanel(panel === 'repository' ? undefined : 'repository')}
              type="button"
            >
              <Plus className="size-4" aria-hidden="true" />
              New repository
            </Button>
            {proposable.length > 0 ? (
              <Button
                onClick={() => setPanel(panel === 'pull-request' ? undefined : 'pull-request')}
                type="button"
                variant="outline"
              >
                <GitPullRequest className="size-4" aria-hidden="true" />
                New pull request
              </Button>
            ) : null}
          </div>
        </div>

        {panel === 'repository' ? (
          <div className="mt-6">
            <CreateRepositoryPanel onClose={() => setPanel(undefined)} />
          </div>
        ) : null}
        {panel === 'pull-request' ? (
          <div className="mt-6">
            <CreatePullRequestPanel
              networkRepositories={snapshot.repositories}
              onClose={() => setPanel(undefined)}
              repositories={proposable}
            />
          </div>
        ) : null}

        <div className="mt-8 grid gap-6 lg:grid-cols-[1.55fr_0.45fr] lg:items-start">
          <section aria-labelledby="your-repositories-title">
            <div className="flex flex-wrap items-center justify-between gap-3 border-b pb-4">
              <h2 className="font-serif text-2xl" id="your-repositories-title">
                Your repositories
              </h2>
              {owned.length > 3 ? (
                <div className="relative w-full max-w-64">
                  <Search
                    aria-hidden="true"
                    className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
                  />
                  <Input
                    aria-label="Filter your repositories"
                    className="h-9 pl-9"
                    onChange={(event) => setFilter(event.target.value)}
                    placeholder="Filter"
                    value={filter}
                  />
                </div>
              ) : null}
            </div>

            {!snapshot.available ? (
              <Alert className="mt-5 bg-card">
                <AlertTitle>Repository list unavailable</AlertTitle>
                <AlertDescription>
                  You are signed in, but this server could not read its repository projection. Your
                  repositories are unaffected and will reappear when the read path recovers.
                </AlertDescription>
              </Alert>
            ) : owned.length === 0 ? (
              <EmptyRepositories onCreate={() => setPanel('repository')} />
            ) : visible.length === 0 ? (
              <p className="mt-5 rounded-lg border border-dashed px-5 py-10 text-center text-sm text-muted-foreground">
                No repositories match “{filter}”.
              </p>
            ) : (
              <ul className="mt-2 divide-y">
                {visible.map((repository) => (
                  <RepositoryRow key={repositoryKey(repository)} repository={repository} />
                ))}
              </ul>
            )}
          </section>

          <aside aria-labelledby="network-title" className="rounded-xl border bg-card p-5">
            <h2 className="font-serif text-xl" id="network-title">
              Latest on this network
            </h2>
            <p className="mt-1 text-xs text-muted-foreground">
              Public repositories as this server indexes them.
            </p>
            <div className="mt-4">
              <LiveRepositories />
            </div>
            <Link
              className={cn(buttonVariants({ size: 'sm', variant: 'outline' }), 'mt-5 w-full')}
              search={{ q: '', sort: 'relevance', type: 'repositories' }}
              to="/explore"
            >
              Explore the network
            </Link>
          </aside>
        </div>
      </main>
    </div>
  )
}

function RepositoryRow({ repository }: { repository: Repository }) {
  const params = repositoryParams(repository)
  return (
    <li className="group py-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <Link
            className="font-serif text-xl underline-offset-4 group-hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            params={params}
            to="/$owner/$repo"
          >
            {repository.display_name ?? repository.slug}
          </Link>
          {repository.description ? (
            <p className="mt-1.5 max-w-2xl text-sm leading-6 text-muted-foreground">
              {repository.description}
            </p>
          ) : null}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {repository.visibility === 'private' ? <Badge variant="outline">Private</Badge> : null}
          {repository.state !== 'active' ? (
            <Badge variant="secondary">{repository.state}</Badge>
          ) : null}
        </div>
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-x-5 gap-y-2 text-xs text-muted-foreground">
        <span className="flex items-center gap-1.5">
          <GitBranch className="size-3.5" aria-hidden="true" />
          {repository.default_branch}
        </span>
        <span className="flex items-center gap-1.5">
          <Star className="size-3.5" aria-hidden="true" />
          {repository.star_count}
        </span>
        <Link
          className="flex items-center gap-1.5 underline-offset-4 hover:text-foreground hover:underline"
          params={params}
          to="/$owner/$repo/issues"
        >
          <CircleDot className="size-3.5" aria-hidden="true" />
          {pluralize(repository.open_issue_count, 'open issue', 'open issues')}
        </Link>
        <Link
          className="flex items-center gap-1.5 underline-offset-4 hover:text-foreground hover:underline"
          params={params}
          to="/$owner/$repo/pulls"
        >
          <GitPullRequest className="size-3.5" aria-hidden="true" />
          {pluralize(repository.open_pull_request_count, 'open pull request', 'open pull requests')}
        </Link>
      </div>
    </li>
  )
}

function EmptyRepositories({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="mt-5 rounded-xl border border-dashed px-6 py-12 text-center">
      <h3 className="font-serif text-2xl">No repositories yet</h3>
      <p className="mx-auto mt-2 max-w-md text-sm leading-6 text-muted-foreground">
        A repository here is a Git remote you can push to and a record published to your identity,
        so the rest of the network can find it.
      </p>
      <Button className="mt-5" onClick={onCreate} type="button">
        <Plus className="size-4" aria-hidden="true" />
        Create a repository
      </Button>
      <p className="mt-5 text-xs text-muted-foreground">
        This list is read from the public network index, so private repositories do not appear here
        yet.
      </p>
    </div>
  )
}
