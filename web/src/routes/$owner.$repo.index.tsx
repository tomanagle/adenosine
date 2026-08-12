import { createFileRoute } from '@tanstack/react-router'

import { RepositoryOverview } from '@/features/repository-browser/code-browser'
import { loadOverview } from '@/features/repository-browser/loaders'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'

export const Route = createFileRoute('/$owner/$repo/')({
  ssr: false,
  loader: ({ context, params }) => loadOverview(context.queryClient, params),
  pendingComponent: RepositoryPending,
  errorComponent: ({ error }) => <RepositoryError error={error} />,
  component: OverviewRoute,
})

function OverviewRoute() {
  return <RepositoryOverview params={Route.useParams()} />
}
