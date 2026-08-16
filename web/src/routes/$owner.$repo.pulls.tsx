import { createFileRoute } from '@tanstack/react-router'

import { PullRequestsPage } from '@/features/collaboration/pages'
import {
  pullRequestsQueryOptions,
  repositoryLabelsQueryOptions,
  repositoryMilestonesQueryOptions,
} from '@/features/collaboration/queries'
import { pullRequestFiltersSchema } from '@/features/collaboration/validation'
import { repositoryQueryOptions } from '@/features/repository-browser/queries'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'

export const Route = createFileRoute('/$owner/$repo/pulls')({
  ssr: false,
  validateSearch: pullRequestFiltersSchema,
  loaderDeps: ({ search }) => search,
  loader: async ({ context, params, deps }) => {
    const repository = await context.queryClient.ensureQueryData(repositoryQueryOptions(params))
    await Promise.all([
      context.queryClient.ensureQueryData(pullRequestsQueryOptions(repository.uri ?? '', deps)),
      context.queryClient.ensureQueryData(repositoryLabelsQueryOptions(params.owner, params.repo)),
      context.queryClient.ensureQueryData(
        repositoryMilestonesQueryOptions(params.owner, params.repo),
      ),
    ])
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
      filters={Route.useSearch()}
    />
  )
}
