import { createFileRoute } from '@tanstack/react-router'

import { ActivityPage } from '@/features/collaboration/pages'
import {
  activityStarsQueryOptions,
  issuesQueryOptions,
  pullRequestsQueryOptions,
} from '@/features/collaboration/queries'
import { repositoryQueryOptions } from '@/features/repository-browser/queries'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'

export const Route = createFileRoute('/$owner/$repo/activity')({
  ssr: false,
  loader: async ({ context, params }) => {
    const repository = await context.queryClient.ensureQueryData(repositoryQueryOptions(params))
    await Promise.all([
      context.queryClient.ensureQueryData(issuesQueryOptions(repository.uri ?? '')),
      context.queryClient.ensureQueryData(pullRequestsQueryOptions(repository.uri ?? '')),
      context.queryClient.ensureQueryData(activityStarsQueryOptions(repository.uri ?? '')),
    ])
  },
  pendingComponent: RepositoryPending,
  errorComponent: ({ error }) => <RepositoryError error={error} />,
  component: ActivityRoute,
})

function ActivityRoute() {
  return <ActivityPage params={Route.useParams()} />
}
