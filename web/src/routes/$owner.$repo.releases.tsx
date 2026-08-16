import { createFileRoute } from '@tanstack/react-router'

import { ReleasesPage } from '@/features/repository-browser/releases'
import { releasesQueryOptions, tagsQueryOptions } from '@/features/repository-browser/queries'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'

export const Route = createFileRoute('/$owner/$repo/releases')({
  ssr: false,
  loader: async ({ context, params }) => {
    const releases = await context.queryClient.ensureQueryData(releasesQueryOptions(params))
    if (releases.viewer_can_manage)
      await context.queryClient.ensureQueryData(tagsQueryOptions(params))
  },
  pendingComponent: RepositoryPending,
  errorComponent: ({ error }) => <RepositoryError error={error} />,
  component: ReleasesRoute,
})

function ReleasesRoute() {
  return <ReleasesPage params={Route.useParams()} />
}
