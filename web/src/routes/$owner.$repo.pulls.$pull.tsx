import { createFileRoute } from '@tanstack/react-router'

import { decodeRecordIdentity } from '@/features/collaboration/identity'
import { PullRequestPage } from '@/features/collaboration/pages'
import { shouldLoadVerifiedPullRequestDiff } from '@/features/collaboration/remote-pr'
import {
  pullRequestDiffQueryOptions,
  pullRequestQueryOptions,
  reviewsQueryOptions,
} from '@/features/collaboration/queries'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'
import { repositoryQueryOptions } from '@/features/repository-browser/queries'

export const Route = createFileRoute('/$owner/$repo/pulls/$pull')({
  ssr: false,
  params: { parse: (params) => ({ ...params, pull: decodeRecordIdentity(params.pull) }) },
  loader: async ({ context, params }) => {
    const repository = await context.queryClient.ensureQueryData(repositoryQueryOptions(params))
    const reads: Array<Promise<unknown>> = [
      context.queryClient.ensureQueryData(pullRequestQueryOptions(params.pull)),
      context.queryClient.ensureQueryData(reviewsQueryOptions(params.pull)),
    ]
    if (shouldLoadVerifiedPullRequestDiff(repository.hosting.source_browsing))
      reads.push(context.queryClient.ensureQueryData(pullRequestDiffQueryOptions(params.pull)))
    await Promise.all(reads)
  },
  pendingComponent: RepositoryPending,
  errorComponent: ({ error }) => <RepositoryError error={error} />,
  component: PullRoute,
})

function PullRoute() {
  const params = Route.useParams()
  return (
    <PullRequestPage
      identityDid={Route.useRouteContext().identity?.did}
      params={params}
      pullRequestUri={params.pull}
    />
  )
}
