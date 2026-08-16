import { createFileRoute } from '@tanstack/react-router'

import { LabelsPage } from '@/features/collaboration/triage-management'
import { repositoryLabelsQueryOptions } from '@/features/collaboration/queries'
import { repositoryQueryOptions } from '@/features/repository-browser/queries'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'

export const Route = createFileRoute('/$owner/$repo/labels')({
  ssr: false,
  loader: async ({ context, params }) => {
    await Promise.all([
      context.queryClient.ensureQueryData(repositoryQueryOptions(params)),
      context.queryClient.ensureQueryData(repositoryLabelsQueryOptions(params.owner, params.repo)),
    ])
  },
  pendingComponent: RepositoryPending,
  errorComponent: ({ error }) => <RepositoryError error={error} />,
  component: LabelsRoute,
})

function LabelsRoute() {
  return <LabelsPage params={Route.useParams()} />
}
