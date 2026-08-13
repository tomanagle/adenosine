import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { CircleDot, GitMerge, Star } from 'lucide-react'
import { useState, type FormEvent } from 'react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { SafeMarkdown } from '@/features/repository-browser/markdown'
import {
  repositoryQueryOptions,
  type RepositoryRouteParams,
} from '@/features/repository-browser/queries'
import { EmptyState } from '@/features/repository-browser/states'

import { encodeRecordIdentity } from './identity'
import { boundedCommentDepth } from './comments'
import { canTriageIssue } from './permissions'
import { PublicationNotice, usePublication } from './publication'
import {
  activityStarsQueryOptions,
  commentsQueryOptions,
  createCommentMutationOptions,
  createIssueMutationOptions,
  createPullMutationOptions,
  createReviewMutationOptions,
  issueQueryOptions,
  issuesQueryOptions,
  issueStatusMutationOptions,
  mergeMutationOptions,
  pullRequestDiffQueryOptions,
  pullRequestQueryOptions,
  pullRequestsQueryOptions,
  pullStatusMutationOptions,
  reviewsQueryOptions,
} from './queries'

type PageProps = { params: RepositoryRouteParams; identityDid?: string }

export function IssuesPage({ params, identityDid }: PageProps) {
  const { data: repository } = useSuspenseQuery(repositoryQueryOptions(params))
  const { data } = useSuspenseQuery(issuesQueryOptions(repository.uri ?? ''))
  const queryClient = useQueryClient()
  const mutation = useMutation(createIssueMutationOptions())
  const publication = usePublication()

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!repository.uri) return
    const form = new FormData(event.currentTarget)
    await publication.publish(
      async () => {
        const result = await mutation.mutateAsync({
          body: {
            repository_uri: repository.uri!,
            title: String(form.get('title')),
            body: String(form.get('body')),
          },
        })
        return result.issue
      },
      async (reference) => {
        const projection = await queryClient.fetchQuery({
          ...issuesQueryOptions(repository.uri!),
          staleTime: 0,
        })
        return projection.items.some(
          (value) => value.uri === reference.uri && value.cid === reference.cid,
        )
      },
    )
    event.currentTarget.reset()
    await queryClient.invalidateQueries({ queryKey: issuesQueryOptions(repository.uri).queryKey })
  }

  return (
    <section className="space-y-5">
      <header className="flex items-end justify-between gap-4">
        <div>
          <h2 className="font-serif text-3xl">Issues</h2>
          <p className="text-sm text-muted-foreground">
            {data.open_issue_count} open of {data.issue_count}
          </p>
        </div>
      </header>
      {identityDid && repository.uri ? (
        <form
          className="grid gap-3 rounded-lg border bg-card p-4"
          onSubmit={(event) => void submit(event)}
        >
          <label className="text-sm font-medium">
            New issue
            <input
              className="mt-1 block h-10 w-full rounded-md border bg-background px-3"
              maxLength={255}
              name="title"
              required
            />
          </label>
          <label className="text-sm font-medium">
            Description
            <textarea
              className="mt-1 block min-h-28 w-full rounded-md border bg-background p-3"
              maxLength={65535}
              name="body"
            />
          </label>
          <div className="flex items-center justify-between gap-3">
            <PublicationNotice state={publication.state} />
            <Button disabled={mutation.isPending} type="submit">
              {mutation.isPending ? 'Publishing...' : 'Open issue'}
            </Button>
          </div>
        </form>
      ) : null}
      {data.items.length ? (
        <ul className="divide-y rounded-lg border bg-card">
          {data.items.map((issue) => (
            <li className="p-4" key={issue.uri}>
              <Link
                className="font-medium hover:underline"
                params={{ ...params, issue: encodeRecordIdentity(issue.uri) }}
                to="/$owner/$repo/issues/$issue"
              >
                {issue.title || 'Untitled issue'}
              </Link>
              <div className="mt-2 flex flex-wrap gap-3 text-xs text-muted-foreground">
                <Badge variant="outline">{issue.state}</Badge>
                <span>{issue.comment_count} comments</span>
                <ProfileLink did={issue.author_did} />
              </div>
            </li>
          ))}
        </ul>
      ) : (
        <EmptyState
          title="No issues"
          description="No visible issue records are indexed for this repository."
        />
      )}
    </section>
  )
}

export function IssuePage({ params, issueUri, identityDid }: PageProps & { issueUri: string }) {
  const { data: repository } = useSuspenseQuery(repositoryQueryOptions(params))
  const { data: issue } = useSuspenseQuery(issueQueryOptions(repository.uri ?? '', issueUri))
  const { data: comments } = useSuspenseQuery(commentsQueryOptions(issueUri))
  const queryClient = useQueryClient()
  const commentMutation = useMutation(createCommentMutationOptions())
  const statusMutation = useMutation(issueStatusMutationOptions())
  const publication = usePublication()
  const canTriage = canTriageIssue({
    local: repository.hosting.local,
    ownerDid: repository.owner.did,
    viewerDid: identityDid,
  })
  const [replyTo, setReplyTo] = useState<string>()

  async function comment(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    await publication.publish(
      async () =>
        (
          await commentMutation.mutateAsync({
            body: { issue_uri: issueUri, parent_uri: replyTo, body: String(form.get('body')) },
          })
        ).comment,
      async (reference) => {
        const projection = await queryClient.fetchQuery({
          ...commentsQueryOptions(issueUri),
          staleTime: 0,
        })
        return projection.items.some(
          (value) => value.uri === reference.uri && value.cid === reference.cid,
        )
      },
    )
    event.currentTarget.reset()
    setReplyTo(undefined)
    await queryClient.invalidateQueries({ queryKey: commentsQueryOptions(issueUri).queryKey })
  }
  async function setState() {
    await publication.publish(
      async () =>
        (
          await statusMutation.mutateAsync({
            body: { issue_uri: issueUri, state: issue.state === 'open' ? 'closed' : 'open' },
          })
        ).status,
      async (reference) => {
        const projection = await queryClient.fetchQuery({
          ...issueQueryOptions(repository.uri ?? '', issueUri),
          staleTime: 0,
        })
        return projection.status_uri === reference.uri && projection.status_cid === reference.cid
      },
    )
    await queryClient.invalidateQueries({
      queryKey: issueQueryOptions(repository.uri ?? '', issueUri).queryKey,
    })
  }

  return (
    <article className="space-y-5">
      <header className="rounded-lg border bg-card p-5">
        <div className="flex flex-wrap items-center gap-2">
          <Badge>{issue.state}</Badge>
          <ProfileLink did={issue.author_did} />
          <span className="text-xs text-muted-foreground">
            opened {formatDate(issue.created_at)}
          </span>
        </div>
        <h2 className="mt-3 font-serif text-3xl">{issue.title || 'Untitled issue'}</h2>
        {canTriage ? (
          <Button className="mt-4" onClick={() => void setState()} size="sm" variant="outline">
            {issue.state === 'open' ? 'Close issue' : 'Reopen issue'}
          </Button>
        ) : null}
      </header>
      <section className="rounded-lg border bg-card">
        <SafeMarkdown source={issue.body} />
      </section>
      <section aria-labelledby="comments-title" className="space-y-3">
        <h3 className="font-serif text-2xl" id="comments-title">
          Comments
        </h3>
        {comments.items.map((value) => (
          <article
            className={
              boundedCommentDepth(value)
                ? 'ml-5 rounded-lg border bg-card sm:ml-10'
                : 'rounded-lg border bg-card'
            }
            key={value.uri}
          >
            <header className="border-b px-5 py-3 text-xs text-muted-foreground">
              <ProfileLink did={value.author_did} /> · {formatDate(value.created_at)}
              {identityDid && !value.parent_uri ? (
                <button
                  className="ml-3 underline underline-offset-4"
                  onClick={() => setReplyTo(value.uri)}
                  type="button"
                >
                  Reply
                </button>
              ) : null}
            </header>
            <SafeMarkdown source={value.body} />
          </article>
        ))}
      </section>
      {identityDid ? (
        <form
          className="grid gap-3 rounded-lg border bg-card p-4"
          onSubmit={(event) => void comment(event)}
        >
          <label className="text-sm font-medium">
            {replyTo ? 'Reply to comment' : 'Add a comment'}
            <textarea
              className="mt-1 block min-h-28 w-full rounded-md border bg-background p-3"
              maxLength={65535}
              name="body"
              required
            />
          </label>
          <div className="flex justify-between gap-3">
            <PublicationNotice state={publication.state} />
            {replyTo ? (
              <Button onClick={() => setReplyTo(undefined)} type="button" variant="ghost">
                Cancel reply
              </Button>
            ) : null}
            <Button disabled={commentMutation.isPending}>Comment</Button>
          </div>
        </form>
      ) : null}
    </article>
  )
}

export function PullRequestsPage({ params, identityDid }: PageProps) {
  const { data: repository } = useSuspenseQuery(repositoryQueryOptions(params))
  const { data } = useSuspenseQuery(pullRequestsQueryOptions(repository.uri ?? ''))
  const mutation = useMutation(createPullMutationOptions())
  const publication = usePublication()
  const queryClient = useQueryClient()
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    if (!repository.uri) return
    await publication.publish(
      async () =>
        (
          await mutation.mutateAsync({
            body: {
              source_repository_uri: String(form.get('source_repository_uri')),
              target_repository_uri: repository.uri!,
              source_branch: String(form.get('source_branch')),
              target_branch: String(form.get('target_branch')),
              head_sha: String(form.get('head_sha')),
              title: String(form.get('title')),
              body: String(form.get('body')),
            },
          })
        ).pull_request,
      async (reference) => {
        const projection = await queryClient.fetchQuery({
          ...pullRequestsQueryOptions(repository.uri!),
          staleTime: 0,
        })
        return projection.items.some(
          (value) => value.uri === reference.uri && value.cid === reference.cid,
        )
      },
    )
    event.currentTarget.reset()
    await queryClient.invalidateQueries({
      queryKey: pullRequestsQueryOptions(repository.uri).queryKey,
    })
  }
  return (
    <section className="space-y-5">
      <header>
        <h2 className="font-serif text-3xl">Pull requests</h2>
        <p className="text-sm text-muted-foreground">
          {data.open_pull_request_count} open of {data.pull_request_count}
        </p>
      </header>
      {identityDid && repository.uri ? (
        <details className="rounded-lg border bg-card p-4">
          <summary className="cursor-pointer font-medium">Open pull request</summary>
          <form className="mt-4 grid gap-3 sm:grid-cols-2" onSubmit={(event) => void submit(event)}>
            {['source_repository_uri', 'source_branch', 'target_branch', 'head_sha', 'title'].map(
              (name) => (
                <label
                  className={name === 'title' ? 'sm:col-span-2 text-sm' : 'text-sm'}
                  key={name}
                >
                  {name.replaceAll('_', ' ')}
                  <input
                    className="mt-1 h-10 w-full rounded-md border bg-background px-3 font-mono text-sm"
                    defaultValue={name === 'target_branch' ? repository.default_branch : undefined}
                    name={name}
                    required
                  />
                </label>
              ),
            )}
            <label className="text-sm sm:col-span-2">
              Description
              <textarea
                className="mt-1 min-h-24 w-full rounded-md border bg-background p-3"
                name="body"
              />
            </label>
            <Button className="sm:col-start-2" disabled={mutation.isPending}>
              Publish proposal
            </Button>
          </form>
          <PublicationNotice state={publication.state} />
        </details>
      ) : null}
      {data.items.length ? (
        <ul className="divide-y rounded-lg border bg-card">
          {data.items.map((pull) => (
            <li className="p-4" key={pull.uri}>
              <Link
                className="font-medium hover:underline"
                params={{ ...params, pull: encodeRecordIdentity(pull.uri) }}
                to="/$owner/$repo/pulls/$pull"
              >
                {pull.title}
              </Link>
              <p className="mt-2 text-xs text-muted-foreground">
                {pull.source_branch} → {pull.target_branch} · {pull.review_count} reviews ·{' '}
                <ProfileLink did={pull.author_did} />
              </p>
            </li>
          ))}
        </ul>
      ) : (
        <EmptyState
          title="No pull requests"
          description="No visible proposals are indexed for this repository."
        />
      )}
    </section>
  )
}

export function PullRequestPage({
  params,
  pullRequestUri,
  identityDid,
}: PageProps & { pullRequestUri: string }) {
  const { data: repository } = useSuspenseQuery(repositoryQueryOptions(params))
  const { data: pull } = useSuspenseQuery(pullRequestQueryOptions(pullRequestUri))
  const { data: reviews } = useSuspenseQuery(reviewsQueryOptions(pullRequestUri))
  const queryClient = useQueryClient()
  const reviewMutation = useMutation(createReviewMutationOptions())
  const statusMutation = useMutation(pullStatusMutationOptions())
  const mergeMutation = useMutation(mergeMutationOptions())
  const publication = usePublication()
  const canTriage = identityDid === repository.owner.did && repository.hosting.local
  async function review(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    await publication.publish(
      async () =>
        (
          await reviewMutation.mutateAsync({
            body: {
              pull_request_uri: pullRequestUri,
              verdict: form.get('verdict') as 'comment' | 'approve' | 'request_changes',
              body: String(form.get('body')),
            },
          })
        ).review,
      async (reference) => {
        const projection = await queryClient.fetchQuery({
          ...reviewsQueryOptions(pullRequestUri),
          staleTime: 0,
        })
        return projection.items.some(
          (value) => value.uri === reference.uri && value.cid === reference.cid,
        )
      },
    )
    event.currentTarget.reset()
    await queryClient.invalidateQueries({ queryKey: reviewsQueryOptions(pullRequestUri).queryKey })
  }
  async function status() {
    await publication.publish(
      async () =>
        (
          await statusMutation.mutateAsync({
            body: {
              pull_request_uri: pullRequestUri,
              state: pull.state === 'open' ? 'closed' : 'open',
            },
          })
        ).status,
      async (reference) => {
        const projection = await queryClient.fetchQuery({
          ...pullRequestQueryOptions(pullRequestUri),
          staleTime: 0,
        })
        return projection.status_uri === reference.uri && projection.status_cid === reference.cid
      },
    )
    await queryClient.invalidateQueries({
      queryKey: pullRequestQueryOptions(pullRequestUri).queryKey,
    })
  }
  async function merge(strategy: 'merge-commit' | 'squash') {
    await mergeMutation.mutateAsync({ body: { pull_request_uri: pullRequestUri, strategy } })
    await queryClient.invalidateQueries({
      queryKey: pullRequestQueryOptions(pullRequestUri).queryKey,
    })
  }
  return (
    <article className="space-y-5">
      <header className="rounded-lg border bg-card p-5">
        <div className="flex flex-wrap gap-2">
          <Badge>{pull.state}</Badge>
          <ProfileLink did={pull.author_did} />
        </div>
        <h2 className="mt-3 font-serif text-3xl">{pull.title}</h2>
        <p className="mt-2 font-mono text-xs text-muted-foreground">
          {pull.source_branch} → {pull.target_branch} at {pull.head_sha.slice(0, 12)}
        </p>
        {canTriage && pull.state === 'open' ? (
          <div className="mt-4 flex flex-wrap gap-2">
            <Button onClick={() => void merge('merge-commit')}>
              <GitMerge className="size-4" /> Merge commit
            </Button>
            <Button onClick={() => void merge('squash')} variant="outline">
              Squash
            </Button>
            <Button onClick={() => void status()} variant="outline">
              Close
            </Button>
          </div>
        ) : null}
      </header>
      <section className="rounded-lg border bg-card">
        <SafeMarkdown source={pull.body} />
      </section>
      {repository.hosting.local ? (
        <VerifiedPullRequestDiff pullRequestUri={pullRequestUri} />
      ) : (
        <section className="rounded-lg border border-dashed p-6">
          <h3 className="font-serif text-2xl">Verified diff unavailable here</h3>
          <p className="mt-2 text-sm text-muted-foreground">
            Only the canonical target host can fetch and verify this pull request's Git objects.
          </p>
          <a
            className="mt-4 inline-block underline underline-offset-4"
            href={repository.hosting.web_url}
            rel="noopener noreferrer"
            target="_blank"
          >
            Open canonical host<span className="sr-only"> (opens in a new tab)</span>
          </a>
        </section>
      )}
      <section className="space-y-3">
        <h3 className="font-serif text-2xl">Reviews</h3>
        {reviews.items.map((value) => (
          <article className="rounded-lg border bg-card" key={value.uri}>
            <header className="border-b px-5 py-3 text-xs">
              <Badge variant="outline">{value.verdict.replaceAll('_', ' ')}</Badge>{' '}
              <ProfileLink did={value.author_did} />
            </header>
            <SafeMarkdown source={value.body} />
          </article>
        ))}
      </section>
      {identityDid ? (
        <form
          className="grid gap-3 rounded-lg border bg-card p-4"
          onSubmit={(event) => void review(event)}
        >
          <label className="text-sm">
            Verdict
            <select
              className="mt-1 block h-10 w-full rounded-md border bg-background px-3"
              name="verdict"
            >
              <option value="comment">Comment</option>
              <option value="approve">Approve</option>
              <option value="request_changes">Request changes</option>
            </select>
          </label>
          <label className="text-sm">
            Review
            <textarea
              className="mt-1 min-h-24 w-full rounded-md border bg-background p-3"
              name="body"
            />
          </label>
          <Button disabled={reviewMutation.isPending}>Submit review</Button>
          <PublicationNotice state={publication.state} />
        </form>
      ) : null}
    </article>
  )
}

function VerifiedPullRequestDiff({ pullRequestUri }: { pullRequestUri: string }) {
  const { data: diff } = useSuspenseQuery(pullRequestDiffQueryOptions(pullRequestUri))
  return (
    <section className="rounded-lg border bg-card p-5">
      <h3 className="font-serif text-2xl">Verified diff</h3>
      <p className="mt-1 text-xs text-muted-foreground">
        Merge base {diff.merge_base.slice(0, 12)} · {diff.diff.files.length} files
      </p>
      <pre
        className="mt-4 max-h-[50rem] overflow-auto whitespace-pre p-4 text-xs"
        aria-label="Pull request patch"
      >
        <code>{diff.diff.patch}</code>
      </pre>
    </section>
  )
}

export function ActivityPage({ params }: PageProps) {
  const { data: repository } = useSuspenseQuery(repositoryQueryOptions(params))
  const { data: issues } = useSuspenseQuery(issuesQueryOptions(repository.uri ?? ''))
  const { data: pulls } = useSuspenseQuery(pullRequestsQueryOptions(repository.uri ?? ''))
  const { data: stars } = useSuspenseQuery(activityStarsQueryOptions(repository.uri ?? ''))
  const items = [
    ...issues.items.map((value) => ({
      key: value.uri,
      at: value.updated_at,
      icon: CircleDot,
      text: `Issue: ${value.title}`,
      did: value.author_did,
    })),
    ...pulls.items.map((value) => ({
      key: value.uri,
      at: value.updated_at,
      icon: GitMerge,
      text: `Pull request: ${value.title}`,
      did: value.author_did,
    })),
    ...stars.items.map((value) => ({
      key: value.uri,
      at: value.created_at,
      icon: Star,
      text: 'Starred the repository',
      did: value.author_did,
    })),
  ]
    .sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
    .slice(0, 100)
  return (
    <section>
      <h2 className="font-serif text-3xl">Activity</h2>
      <p className="mt-1 text-sm text-muted-foreground">
        A bounded view composed from this instance's moderated collaboration projections.
      </p>
      {items.length ? (
        <ol className="mt-5 divide-y rounded-lg border bg-card">
          {items.map((item) => (
            <li className="flex gap-3 p-4" key={item.key}>
              <item.icon className="mt-0.5 size-4" />
              <div>
                <p>{item.text}</p>
                <p className="mt-1 text-xs text-muted-foreground">
                  <ProfileLink did={item.did} /> · {formatDate(item.at)}
                </p>
              </div>
            </li>
          ))}
        </ol>
      ) : (
        <EmptyState
          title="No activity"
          description="No visible collaboration records are indexed yet."
        />
      )}
    </section>
  )
}

function ProfileLink({ did }: { did: string }) {
  return (
    <Link
      className="font-mono text-xs underline-offset-4 hover:underline"
      params={{ identity: did }}
      to="/profiles/$identity"
    >
      {did}
    </Link>
  )
}
function formatDate(value: string) {
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(new Date(value))
}
