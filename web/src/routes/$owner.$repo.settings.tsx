import { createFileRoute } from '@tanstack/react-router'

import { RepositorySettings } from '@/features/repository-browser/repository-settings'
import {
  branchProtectionsQueryOptions,
  repositoryQueryOptions,
  repositoryWebhooksQueryOptions,
} from '@/features/repository-browser/queries'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'

export const Route = createFileRoute('/$owner/$repo/settings')({
  ssr: false,
  loader: async ({ context, params }) => {
    const repository = await context.queryClient.ensureQueryData(repositoryQueryOptions(params))
    if (repository.viewer_can_admin) {
      await Promise.all([
        context.queryClient.ensureQueryData(repositoryWebhooksQueryOptions(params)),
        context.queryClient.ensureQueryData(branchProtectionsQueryOptions(params)),
      ])
    }
  },
  pendingComponent: RepositoryPending,
  errorComponent: ({ error }) => <RepositoryError error={error} />,
  component: SettingsRoute,
})

function SettingsRoute() {
  return <RepositorySettings params={Route.useParams()} />
}
