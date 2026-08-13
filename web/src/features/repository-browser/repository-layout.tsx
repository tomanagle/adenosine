import { useState } from 'react'
import { useMutation, useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { Link, Outlet } from '@tanstack/react-router'
import {
  Activity,
  Check,
  ChevronDown,
  Clipboard,
  Code2,
  CircleDot,
  ExternalLink,
  GitBranch,
  GitCompareArrows,
  GitPullRequest,
  History,
  Lock,
  Star,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
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
import { addAcceptedStar, optimisticStarState, removeAcceptedStar } from './star-cache'
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
  const branches = useQuery({
    ...branchesQueryOptions(params),
    enabled: repository.hosting.source_browsing === 'local',
  })
  const tags = useQuery({
    ...tagsQueryOptions(params),
    enabled: repository.hosting.source_browsing === 'local',
  })
  const stars = useQuery({
    ...starsQueryOptions(repository.uri ?? ''),
    enabled: Boolean(repository.uri),
  })
  const putStar = useMutation({
    ...putStarMutationOptions(),
    onSuccess: (mutation) => {
      if (!repository.uri) return
      const query = starsQueryOptions(repository.uri)
      queryClient.setQueryData(query.queryKey, (projection) =>
        addAcceptedStar(
          projection ?? {
            items: [],
            page: { next_cursor: null },
            star_count: repository.star_count,
          },
          mutation.star,
        ),
      )
    },
  })
  const deleteStar = useMutation({
    ...deleteStarMutationOptions(),
    onSuccess: () => {
      if (!repository.uri || !identityDid) return
      const query = starsQueryOptions(repository.uri)
      queryClient.setQueryData(query.queryKey, (projection) =>
        projection ? removeAcceptedStar(projection, identityDid) : projection,
      )
    },
  })
  const [cloneKind, setCloneKind] = useState<'https' | 'ssh'>('https')
  const [copied, setCopied] = useState(false)
  const canonicalUrl = safeWebUrl(repository.hosting.web_url)
  const cloneUrl =
    cloneKind === 'ssh' && repository.hosting.git_ssh_url
      ? repository.hosting.git_ssh_url
      : repository.hosting.git_https_url
  const projectedStarred = Boolean(
    identityDid && stars.data?.items.some((star) => star.author_did === identityDid),
  )
  const starPending = putStar.isPending || deleteStar.isPending
  const starError = putStar.error ?? deleteStar.error
  const { starCount, starred } = optimisticStarState({
    deleting: deleteStar.isPending,
    putting: putStar.isPending,
    starCount: stars.data?.star_count ?? repository.star_count,
    starred: projectedStarred,
  })

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
    <main className="bg-muted/20">
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
                className="mt-3 break-words font-serif text-3xl leading-tight tracking-tight sm:text-4xl"
                id="repository-title"
              >
                <span className="font-normal text-muted-foreground">
                  {repository.owner.handle ?? params.owner} /
                </span>{' '}
                {repository.display_name ?? repository.slug}
              </h1>
              {repository.description ? (
                <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">
                  {repository.description}
                </p>
              ) : null}
            </div>
            <div className="flex shrink-0 flex-col items-start gap-2 sm:items-end">
              <div className="flex flex-wrap gap-2">
                {!identityDid ? (
                  <Link
                    className={cn(buttonVariants({ size: 'sm', variant: 'outline' }))}
                    to="/login"
                  >
                    <Star aria-hidden="true" className="size-3.5" />
                    Sign in to star
                    <span className="tabular-nums">{starCount}</span>
                  </Link>
                ) : (
                  <Button
                    aria-pressed={starred}
                    aria-busy={starPending}
                    disabled={!repository.uri || stars.isPending || starPending}
                    onClick={toggleStar}
                    size="sm"
                    title={
                      !repository.uri
                        ? 'This repository has no publishable network URI'
                        : starred
                          ? 'Remove this repository from your stars'
                          : 'Add this repository to your stars'
                    }
                    variant={starred ? 'default' : 'outline'}
                  >
                    <Star
                      aria-hidden="true"
                      className={cn('size-3.5', starred && 'fill-current')}
                    />
                    {stars.isPending ? 'Loading...' : starred ? 'Starred' : 'Star'}
                    <span className="tabular-nums">{starCount}</span>
                  </Button>
                )}
                <details className="relative">
                  <summary
                    className={cn(buttonVariants({ size: 'sm' }), 'cursor-pointer list-none')}
                  >
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
              {starError ? (
                <p className="max-w-xs text-xs text-danger" role="alert">
                  {apiErrorMessage(starError, 'The star could not be saved. Please try again.')}
                </p>
              ) : null}
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
            <RepositoryNavLink
              icon={CircleDot}
              label="Issues"
              params={params}
              to="/$owner/$repo/issues"
            />
            <RepositoryNavLink
              icon={GitPullRequest}
              label="Pulls"
              params={params}
              to="/$owner/$repo/pulls"
            />
            <RepositoryNavLink
              icon={Activity}
              label="Activity"
              params={params}
              to="/$owner/$repo/activity"
            />
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
                  {branches.isPending ? '...' : (branches.data?.items.length ?? 'Unavailable')}
                </dd>
              </div>
              <div className="flex justify-between gap-3">
                <dt>Tags</dt>
                <dd>{tags.isPending ? '...' : (tags.data?.items.length ?? 'Unavailable')}</dd>
              </div>
              <div className="flex justify-between gap-3">
                <dt>Default</dt>
                <dd className="min-w-0 truncate font-mono text-xs">{repository.default_branch}</dd>
              </div>
            </dl>
            {branches.data?.items.length ? (
              <ul className="mt-3 space-y-1" aria-label="Repository branches">
                {branches.data.items.slice(0, 5).map((branch) => (
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
            {tags.data?.items.length ? (
              <details className="mt-2 text-xs">
                <summary className="cursor-pointer text-muted-foreground">View tags</summary>
                <ul className="mt-1 space-y-1" aria-label="Repository tags">
                  {tags.data.items.slice(0, 5).map((tag) => (
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
  to:
    | '/$owner/$repo'
    | '/$owner/$repo/commits'
    | '/$owner/$repo/compare'
    | '/$owner/$repo/issues'
    | '/$owner/$repo/pulls'
    | '/$owner/$repo/activity'
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
