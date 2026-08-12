import { createFileRoute } from '@tanstack/react-router'

import { IssuesPage } from '@/features/collaboration/pages'
import { issuesQueryOptions } from '@/features/collaboration/queries'
import { repositoryQueryOptions } from '@/features/repository-browser/queries'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'

export const Route = createFileRoute('/$owner/$repo/issues')({
  ssr: false,
  loader: async ({ context, params }) => {
    const repository = await context.queryClient.ensureQueryData(repositoryQueryOptions(params))
    await context.queryClient.ensureQueryData(issuesQueryOptions(repository.uri ?? ''))
  },
  pendingComponent: RepositoryPending,
  errorComponent: ({ error }) => <RepositoryError error={error} />,
  component: IssuesRoute,
})

function IssuesRoute() {
  return (
    <IssuesPage identityDid={Route.useRouteContext().identity?.did} params={Route.useParams()} />
  )
}
