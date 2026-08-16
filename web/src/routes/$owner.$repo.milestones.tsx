import { createFileRoute } from '@tanstack/react-router'

import { repositoryMilestonesQueryOptions } from '@/features/collaboration/queries'
import { MilestonesPage } from '@/features/collaboration/triage-management'
import { repositoryQueryOptions } from '@/features/repository-browser/queries'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'

export const Route = createFileRoute('/$owner/$repo/milestones')({
  ssr: false,
  loader: async ({ context, params }) => {
    await Promise.all([
      context.queryClient.ensureQueryData(repositoryQueryOptions(params)),
      context.queryClient.ensureQueryData(
        repositoryMilestonesQueryOptions(params.owner, params.repo),
      ),
    ])
  },
  pendingComponent: RepositoryPending,
  errorComponent: ({ error }) => <RepositoryError error={error} />,
  component: MilestonesRoute,
})

function MilestonesRoute() {
  return <MilestonesPage params={Route.useParams()} />
}
