import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  Bell,
  Check,
  CircleDot,
  GitMerge,
  GitPullRequest,
  MessageSquare,
  Trash2,
} from 'lucide-react'

import { Button } from '@/components/ui/button'
import { encodeRecordIdentity } from '@/features/collaboration/identity'
import { apiErrorMessage } from '@/lib/api-error'
import { cn } from '@/lib/utils'

import {
  deleteNotificationMutationOptions,
  notificationsInfiniteQueryOptions,
  notificationsQueryOptions,
  updateNotificationMutationOptions,
} from './queries'

export function NotificationsPage() {
  const queryClient = useQueryClient()
  const query = useInfiniteQuery(notificationsInfiniteQueryOptions())
  const update = useMutation(updateNotificationMutationOptions())
  const dismiss = useMutation(deleteNotificationMutationOptions())
  const items = (query.data?.pages ?? []).flatMap((page) => page.items)
  const refresh = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: notificationsQueryOptions().queryKey }),
      queryClient.invalidateQueries({ queryKey: notificationsQueryOptions(true).queryKey }),
    ])
  }
  return (
    <main className="mx-auto max-w-4xl px-5 py-8 sm:px-8 sm:py-12">
      <header className="border-b pb-5">
        <p className="text-xs font-medium uppercase tracking-[0.16em] text-primary">Inbox</p>
        <h1 className="mt-1 flex items-center gap-3 font-serif text-4xl">
          <Bell className="size-7" /> Notifications
        </h1>
        <p className="mt-2 text-sm text-muted-foreground">
          Mentions and activity on work you started. Read state stays on this server.
        </p>
      </header>
      {query.isPending ? (
        <p className="py-10 text-sm text-muted-foreground">Loading notifications…</p>
      ) : null}
      {query.isError ? (
        <p className="py-10 text-sm text-danger" role="alert">
          {apiErrorMessage(query.error, 'Notifications could not be loaded.')}
        </p>
      ) : null}
      {!query.isPending && items.length === 0 ? (
        <div className="py-16 text-center">
          <Bell className="mx-auto size-8 text-muted-foreground" />
          <h2 className="mt-4 font-serif text-2xl">You’re all caught up</h2>
          <p className="mt-2 text-sm text-muted-foreground">
            New mentions, reviews, merges, and issue replies will appear here.
          </p>
        </div>
      ) : null}
      <ul className="divide-y" aria-label="Notifications">
        {items.map((item) => {
          const target =
            item.subject_kind === 'issue'
              ? ('/$owner/$repo/issues/$issue' as const)
              : ('/$owner/$repo/pulls/$pull' as const)
          const icon =
            item.kind === 'issue_comment'
              ? MessageSquare
              : item.kind === 'pull_request_merged'
                ? GitMerge
                : item.subject_kind === 'issue'
                  ? CircleDot
                  : GitPullRequest
          const Icon = icon
          const label =
            item.kind === 'mention'
              ? 'mentioned you'
              : item.kind === 'issue_comment'
                ? 'replied to your issue'
                : item.kind === 'pull_request_review'
                  ? 'reviewed your pull request'
                  : item.kind === 'pull_request_review_request'
                    ? 'requested your review'
                    : 'merged your pull request'
          const record = encodeRecordIdentity(item.subject_uri)
          return (
            <li className={cn('flex gap-3 py-5', !item.read && 'bg-primary/[0.035]')} key={item.id}>
              <div
                className={cn(
                  'mt-0.5 grid size-9 shrink-0 place-items-center rounded-full',
                  item.read ? 'bg-muted text-muted-foreground' : 'bg-primary/15 text-primary',
                )}
              >
                <Icon className="size-4" />
              </div>
              <div className="min-w-0 flex-1">
                <p className="text-sm">
                  <span className="font-medium">{item.actor_did}</span> {label}
                </p>
                <Link
                  className="mt-1 block truncate font-medium underline-offset-4 hover:underline"
                  params={
                    item.subject_kind === 'issue'
                      ? { owner: item.owner, repo: item.repository_slug, issue: record }
                      : { owner: item.owner, repo: item.repository_slug, pull: record }
                  }
                  to={target}
                >
                  {item.title}
                </Link>
                <p className="mt-1 text-xs text-muted-foreground">
                  {item.owner}/{item.repository_slug} ·{' '}
                  {new Date(item.occurred_at).toLocaleString()}
                </p>
              </div>
              <div className="flex shrink-0 items-start gap-1">
                {!item.read ? (
                  <Button
                    aria-label="Mark read"
                    size="sm"
                    variant="ghost"
                    disabled={update.isPending}
                    onClick={() => {
                      void update
                        .mutateAsync({ path: { notification: item.id }, body: { read: true } })
                        .then(refresh)
                    }}
                  >
                    <Check className="size-4" />
                  </Button>
                ) : null}
                <Button
                  aria-label="Dismiss"
                  size="sm"
                  variant="ghost"
                  disabled={dismiss.isPending}
                  onClick={() => {
                    void dismiss.mutateAsync({ path: { notification: item.id } }).then(refresh)
                  }}
                >
                  <Trash2 className="size-4" />
                </Button>
              </div>
            </li>
          )
        })}
      </ul>
      {query.hasNextPage ? (
        <div className="mt-6 text-center">
          <Button
            variant="outline"
            disabled={query.isFetchingNextPage}
            onClick={() => void query.fetchNextPage()}
          >
            {query.isFetchingNextPage ? 'Loading…' : 'Load more'}
          </Button>
        </div>
      ) : null}
    </main>
  )
}
