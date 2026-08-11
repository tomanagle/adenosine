import { useState } from 'react'
import { useMutation, useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { Link, Outlet } from '@tanstack/react-router'
import {
  Check,
  ChevronDown,
  Clipboard,
  Code2,
  ExternalLink,
  GitBranch,
  GitCompareArrows,
  History,
  Lock,
  Star,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { cn } from '@/lib/utils'

import {
  branchesQueryOptions,
  deleteStarMutationOptions,
  putStarMutationOptions,
  repositoryQueryOptions,
  starsQueryOptions,
  tagsQueryOptions,
} from './queries'
import type { RepositoryRouteParams } from './queries'
import { hostingLabel, safeWebUrl } from './view-models'

export function RepositoryLayout({
  identityDid,
  params,
}: {
  identityDid?: string
  params: RepositoryRouteParams
}) {
  const { data: repository } = useSuspenseQuery(repositoryQueryOptions(params))
  const queryClient = useQueryClient()
  const branches = useQuery(branchesQueryOptions(params))
  const tags = useQuery(tagsQueryOptions(params))
  const stars = useQuery({
    ...starsQueryOptions(repository.uri ?? ''),
    enabled: Boolean(repository.uri),
  })
  const refreshStars = () =>
    repository.uri
      ? queryClient.invalidateQueries({ queryKey: starsQueryOptions(repository.uri).queryKey })
      : undefined
  const putStar = useMutation({ ...putStarMutationOptions(), onSuccess: refreshStars })
  const deleteStar = useMutation({ ...deleteStarMutationOptions(), onSuccess: refreshStars })
  const [cloneKind, setCloneKind] = useState<'https' | 'ssh'>('https')
  const [copied, setCopied] = useState(false)
  const canonicalUrl = safeWebUrl(repository.hosting.web_url)
  const cloneUrl =
    cloneKind === 'ssh' && repository.hosting.git_ssh_url
      ? repository.hosting.git_ssh_url
      : repository.hosting.git_https_url
  const starred = Boolean(
    identityDid && stars.data?.data.some((star) => star.author_did === identityDid),
  )
  const starPending = putStar.isPending || deleteStar.isPending

  function toggleStar() {
    if (!repository.uri || !identityDid) return
    const options = { query: { repository_uri: repository.uri } }
    if (starred) deleteStar.mutate(options)
    else putStar.mutate(options)
  }

  async function copyCloneUrl() {
    await navigator.clipboard.writeText(cloneUrl)
    setCopied(true)
  }

  return (
    <main className="min-h-screen bg-muted/20">
      <header className="border-b bg-background">
        <div className="mx-auto flex min-h-14 max-w-6xl items-center justify-between gap-4 px-5 py-3 sm:px-8">
          <Link
            to="/"
            className="font-semibold tracking-tight focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            Adenosine
          </Link>
          <span className="text-xs text-muted-foreground">Public federated forge</span>
        </div>
      </header>

      <section className="border-b bg-card" aria-labelledby="repository-title">
        <div className="mx-auto max-w-6xl px-5 pt-6 sm:px-8 sm:pt-8">
          <div className="flex flex-col gap-5 sm:flex-row sm:items-start sm:justify-between">
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant={repository.hosting.local ? 'secondary' : 'outline'}>
                  {hostingLabel(repository)}
                </Badge>
                <Badge variant="outline">
                  {repository.visibility === 'private' ? <Lock className="mr-1 size-3" /> : null}
                  {repository.visibility}
                </Badge>
              </div>
              <h1
                className="mt-3 truncate font-serif text-3xl tracking-tight sm:text-4xl"
                id="repository-title"
              >
                <span className="font-normal text-muted-foreground">
                  {repository.owner.handle ?? repository.owner.did} /
                </span>{' '}
                {repository.display_name ?? repository.slug}
              </h1>
              {repository.description ? (
                <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">
                  {repository.description}
                </p>
              ) : null}
            </div>
            <div className="flex shrink-0 flex-wrap gap-2">
              <Button
                aria-pressed={starred}
                disabled={!identityDid || !repository.uri || starPending}
                onClick={toggleStar}
                size="sm"
                title={
                  !identityDid
                    ? 'Sign in to star this repository'
                    : !repository.uri
                      ? 'This repository has no publishable network URI'
                      : undefined
                }
                variant="outline"
              >
                <Star className={cn('size-3.5', starred && 'fill-current')} />
                {starPending ? 'Publishing...' : starred ? 'Unstar' : 'Star'}
                <span className="tabular-nums">
                  {stars.data?.star_count ?? repository.star_count}
                </span>
              </Button>
              <details className="relative">
                <summary className={cn(buttonVariants({ size: 'sm' }), 'cursor-pointer list-none')}>
                  <Code2 className="size-3.5" /> Clone <ChevronDown className="size-3.5" />
                </summary>
                <div className="absolute right-0 z-20 mt-2 w-[min(22rem,calc(100vw-2.5rem))] rounded-lg border bg-card p-3 shadow-lg">
                  <div className="mb-2 flex gap-1" aria-label="Clone protocol">
                    <Button
                      aria-pressed={cloneKind === 'https'}
                      onClick={() => setCloneKind('https')}
                      size="sm"
                      variant={cloneKind === 'https' ? 'default' : 'ghost'}
                    >
                      HTTPS
                    </Button>
                    {repository.hosting.git_ssh_url ? (
                      <Button
                        aria-pressed={cloneKind === 'ssh'}
                        onClick={() => setCloneKind('ssh')}
                        size="sm"
                        variant={cloneKind === 'ssh' ? 'default' : 'ghost'}
                      >
                        SSH
                      </Button>
                    ) : null}
                  </div>
                  <div className="flex min-w-0 items-center rounded-md border bg-muted/30 pl-3">
                    <code className="min-w-0 flex-1 truncate text-xs">{cloneUrl}</code>
                    <Button
                      aria-label="Copy clone URL"
                      onClick={() => void copyCloneUrl()}
                      size="sm"
                      variant="ghost"
                    >
                      {copied ? <Check className="size-4" /> : <Clipboard className="size-4" />}
                    </Button>
                  </div>
                </div>
              </details>
            </div>
          </div>

          <nav className="mt-6 flex gap-1 overflow-x-auto" aria-label="Repository">
            <RepositoryNavLink icon={Code2} label="Code" params={params} to="/$owner/$repo" />
            <RepositoryNavLink
              icon={History}
              label="Commits"
              params={params}
              to="/$owner/$repo/commits"
            />
            <RepositoryNavLink
              icon={GitCompareArrows}
              label="Compare"
              params={params}
              to="/$owner/$repo/compare"
            />
            {['Issues', 'Pulls', 'Activity'].map((label) =>
              canonicalUrl && !repository.hosting.local ? (
                <a
                  className="inline-flex h-10 shrink-0 items-center gap-1 px-3 text-sm text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  href={canonicalUrl}
                  key={label}
                  rel="noopener noreferrer"
                  target="_blank"
                >
                  {label} <ExternalLink className="size-3" aria-hidden="true" />
                  <span className="sr-only"> on the canonical host (opens in a new tab)</span>
                </a>
              ) : (
                <span
                  aria-disabled="true"
                  className="inline-flex h-10 shrink-0 items-center px-3 text-sm text-muted-foreground"
                  key={label}
                >
                  {label}
                  <span className="sr-only"> (unavailable)</span>
                </span>
              ),
            )}
          </nav>
        </div>
      </section>

      <div className="mx-auto grid max-w-6xl gap-6 px-5 py-6 sm:px-8 lg:grid-cols-[minmax(0,1fr)_15rem]">
        <div className="min-w-0">
          <Outlet />
        </div>
        <aside
          className="space-y-5 border-t pt-5 lg:border-l lg:border-t-0 lg:pl-6 lg:pt-0"
          aria-label="Repository context"
        >
          <div>
            <h2 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              Refs
            </h2>
            <dl className="mt-2 space-y-2 text-sm">
              <div className="flex justify-between gap-3">
                <dt>Branches</dt>
                <dd>
                  {branches.isPending ? '...' : (branches.data?.data.length ?? 'Unavailable')}
                </dd>
              </div>
              <div className="flex justify-between gap-3">
                <dt>Tags</dt>
                <dd>{tags.isPending ? '...' : (tags.data?.data.length ?? 'Unavailable')}</dd>
              </div>
              <div className="flex justify-between gap-3">
                <dt>Default</dt>
                <dd className="min-w-0 truncate font-mono text-xs">{repository.default_branch}</dd>
              </div>
            </dl>
            {branches.data?.data.length ? (
              <ul className="mt-3 space-y-1" aria-label="Repository branches">
                {branches.data.data.slice(0, 5).map((branch) => (
                  <li key={branch.name}>
                    <Link
                      className="flex items-center gap-2 truncate rounded px-1 py-1 font-mono text-xs hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                      params={{ ...params, _splat: '' }}
                      search={{ ref: branch.name }}
                      to="/$owner/$repo/tree/$"
                    >
                      <GitBranch className="size-3 shrink-0" /> {branch.name}
                      {branch.default ? <span className="sr-only"> (default)</span> : null}
                    </Link>
                  </li>
                ))}
              </ul>
            ) : null}
            {tags.data?.data.length ? (
              <details className="mt-2 text-xs">
                <summary className="cursor-pointer text-muted-foreground">View tags</summary>
                <ul className="mt-1 space-y-1" aria-label="Repository tags">
                  {tags.data.data.slice(0, 5).map((tag) => (
                    <li key={tag.name}>
                      <Link
                        className="block truncate rounded px-1 py-1 font-mono hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                        params={{ ...params, _splat: '' }}
                        search={{ ref: tag.name }}
                        to="/$owner/$repo/tree/$"
                      >
                        {tag.name}
                      </Link>
                    </li>
                  ))}
                </ul>
              </details>
            ) : null}
          </div>
          <div>
            <h2 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              Hosting
            </h2>
            <p className="mt-2 text-sm leading-6">{hostingLabel(repository)}</p>
            {!repository.hosting.local ? (
              <p className="mt-2 text-xs leading-5 text-muted-foreground">
                Host-local actions are unavailable here.
              </p>
            ) : null}
            {canonicalUrl ? (
              <a
                className="mt-2 inline-flex items-center gap-1 text-sm underline underline-offset-4"
                href={canonicalUrl}
                rel="noopener noreferrer"
                target="_blank"
              >
                Canonical repository <ExternalLink className="size-3.5" />
              </a>
            ) : null}
          </div>
          <div>
            <h2 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              Collaboration
            </h2>
            <p className="mt-2 text-sm text-muted-foreground">
              {repository.open_issue_count} open issues · {repository.open_pull_request_count} open
              pulls
            </p>
          </div>
        </aside>
      </div>
    </main>
  )
}

function RepositoryNavLink({
  icon: Icon,
  label,
  params,
  to,
}: {
  icon: typeof GitBranch
  label: string
  params: RepositoryRouteParams
  to: '/$owner/$repo' | '/$owner/$repo/commits' | '/$owner/$repo/compare'
}) {
  return (
    <Link
      activeProps={{ className: 'border-foreground text-foreground' }}
      activeOptions={{ exact: label === 'Code' }}
      className="inline-flex h-10 shrink-0 items-center gap-2 border-b-2 border-transparent px-3 text-sm text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      params={params}
      to={to}
    >
      <Icon className="size-4" aria-hidden="true" /> {label}
    </Link>
  )
}
