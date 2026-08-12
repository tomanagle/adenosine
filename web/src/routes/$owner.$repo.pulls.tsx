import { createFileRoute } from '@tanstack/react-router'

import { PullRequestsPage } from '@/features/collaboration/pages'
import { pullRequestsQueryOptions } from '@/features/collaboration/queries'
import { repositoryQueryOptions } from '@/features/repository-browser/queries'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'

export const Route = createFileRoute('/$owner/$repo/pulls')({
  ssr: false,
  loader: async ({ context, params }) => {
    const repository = await context.queryClient.ensureQueryData(repositoryQueryOptions(params))
    await context.queryClient.ensureQueryData(pullRequestsQueryOptions(repository.uri ?? ''))
  },
  pendingComponent: RepositoryPending,
  errorComponent: ({ error }) => <RepositoryError error={error} />,
  component: PullsRoute,
})

function PullsRoute() {
  return (
    <PullRequestsPage
      identityDid={Route.useRouteContext().identity?.did}
      params={Route.useParams()}
    />
  )
}
