import { useQuery, useSuspenseQuery } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { GitCommitHorizontal, GitMerge, UserRound } from 'lucide-react'

import { Button } from '@/components/ui/button'

import {
  commitQueryOptions,
  commitsQueryOptions,
  diffQueryOptions,
  repositoryQueryOptions,
  type RepositoryRouteParams,
} from './queries'
import { DiffPanel } from './repository-diff'
import { EmptyState, RepositoryError } from './states'
import { shortSha } from './view-models'

export function CommitHistory({
  params,
  ref,
  limit,
}: {
  params: RepositoryRouteParams
  ref?: string
  limit: number
}) {
  const navigate = useNavigate()
  const { data: repository } = useSuspenseQuery(repositoryQueryOptions(params))
  const revision = ref ?? repository.default_branch
  const { data: commits } = useSuspenseQuery(commitsQueryOptions(params, revision, limit))

  return (
    <section className="overflow-hidden rounded-lg border bg-card" aria-labelledby="history-title">
      <div className="flex flex-col gap-3 border-b p-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="font-semibold" id="history-title">
            Commit history
          </h2>
          <p className="mt-1 text-xs text-muted-foreground">
            Newest first · at most {limit} commits
          </p>
        </div>
        <label>
          <span className="sr-only">Revision</span>
          <input
            className="h-9 w-full rounded-md border bg-background px-3 font-mono text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring sm:w-56"
            defaultValue={revision}
            key={revision}
            onKeyDown={(event) => {
              if (event.key === 'Enter') {
                void navigate({
                  to: '/$owner/$repo/commits',
                  params,
                  search: { ref: event.currentTarget.value, limit },
                })
              }
            }}
          />
        </label>
      </div>
      {commits.items.length === 0 ? (
        <EmptyState
          title="No commits"
          description="This revision does not contain commit history."
        />
      ) : (
        <ol className="divide-y">
          {commits.items.map((commit) => (
            <li className="p-4" key={commit.sha}>
              <div className="flex gap-3">
                <span className="mt-0.5 grid size-8 shrink-0 place-items-center rounded-full bg-muted">
                  <GitCommitHorizontal className="size-4" aria-hidden="true" />
                </span>
                <div className="min-w-0 flex-1">
                  <Link
                    className="font-medium hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    params={{ ...params, revision: commit.sha }}
                    to="/$owner/$repo/commit/$revision"
                  >
                    {commit.summary || '(no commit summary)'}
                  </Link>
                  <p className="mt-1 flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
                    <span className="inline-flex items-center gap-1">
                      <UserRound className="size-3" /> {commit.author.name}
                    </span>
                    <time dateTime={commit.author.date}>{formatDate(commit.author.date)}</time>
                    {commit.parents.length > 1 ? (
                      <span className="inline-flex items-center gap-1">
                        <GitMerge className="size-3" /> merge
                      </span>
                    ) : null}
                  </p>
                </div>
                <code className="hidden text-xs text-muted-foreground sm:block">
                  {shortSha(commit.sha)}
                </code>
              </div>
            </li>
          ))}
        </ol>
      )}
      {commits.items.length === limit && limit < 100 ? (
        <div className="border-t p-3 text-center">
          <Button
            onClick={() =>
              void navigate({
                to: '/$owner/$repo/commits',
                params,
                search: { ref: revision, limit: Math.min(limit + 30, 100) },
              })
            }
            variant="outline"
          >
            Show more commits
          </Button>
        </div>
      ) : null}
    </section>
  )
}

export function CommitDetail({
  params,
  revision,
}: {
  params: RepositoryRouteParams
  revision: string
}) {
  const { data: commit } = useSuspenseQuery(commitQueryOptions(params, revision))
  const parent = commit.parents[0]
  return (
    <div className="space-y-5">
      <article className="rounded-lg border bg-card p-5 sm:p-6">
        <div className="flex items-start gap-3">
          <GitCommitHorizontal className="mt-1 size-5 shrink-0" aria-hidden="true" />
          <div className="min-w-0">
            <h2 className="text-balance font-serif text-2xl tracking-tight">
              {commit.summary || '(no commit summary)'}
            </h2>
            <p className="mt-2 text-sm text-muted-foreground">
              Authored by {commit.author.name} on{' '}
              <time dateTime={commit.author.date}>{formatDate(commit.author.date)}</time>
            </p>
          </div>
        </div>
        <pre className="mt-5 overflow-x-auto whitespace-pre-wrap border-t pt-5 text-sm leading-6">
          {commit.message}
        </pre>
        <dl className="mt-5 grid gap-2 border-t pt-4 text-xs sm:grid-cols-[6rem_1fr]">
          <dt className="text-muted-foreground">Commit</dt>
          <dd className="overflow-x-auto font-mono">{commit.sha}</dd>
          <dt className="text-muted-foreground">Parent{commit.parents.length === 1 ? '' : 's'}</dt>
          <dd className="flex flex-wrap gap-2 font-mono">
            {commit.parents.length > 0 ? commit.parents.map(shortSha).join(', ') : 'Root commit'}
          </dd>
        </dl>
      </article>
      {parent ? (
        <CommitDiff params={params} base={parent} head={commit.sha} />
      ) : (
        <EmptyState
          title="Root commit"
          description="There is no parent commit to use as a bounded diff base."
        />
      )}
    </div>
  )
}

function CommitDiff({
  params,
  base,
  head,
}: {
  params: RepositoryRouteParams
  base: string
  head: string
}) {
  const diff = useQuery(diffQueryOptions(params, base, head))
  if (diff.isPending) {
    return (
      <output className="block p-6 text-sm text-muted-foreground">Loading bounded diff...</output>
    )
  }
  if (diff.isError) return <RepositoryError error={diff.error} />
  return <DiffPanel diff={diff.data} />
}

function formatDate(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.valueOf())
    ? value
    : new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}
