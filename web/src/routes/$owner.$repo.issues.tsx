import { createFileRoute } from '@tanstack/react-router'

import { IssuesPage } from '@/features/collaboration/pages'
import {
  issuesQueryOptions,
  repositoryLabelsQueryOptions,
  repositoryMilestonesQueryOptions,
} from '@/features/collaboration/queries'
import { issueFiltersSchema } from '@/features/collaboration/validation'
import { repositoryQueryOptions } from '@/features/repository-browser/queries'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'

export const Route = createFileRoute('/$owner/$repo/issues')({
  ssr: false,
  validateSearch: issueFiltersSchema,
  loaderDeps: ({ search }) => search,
  loader: async ({ context, params, deps }) => {
    const repository = await context.queryClient.ensureQueryData(repositoryQueryOptions(params))
    await Promise.all([
      context.queryClient.ensureQueryData(issuesQueryOptions(repository.uri ?? '', deps)),
      context.queryClient.ensureQueryData(repositoryLabelsQueryOptions(params.owner, params.repo)),
      context.queryClient.ensureQueryData(
        repositoryMilestonesQueryOptions(params.owner, params.repo),
      ),
    ])
  },
  pendingComponent: RepositoryPending,
  errorComponent: ({ error }) => <RepositoryError error={error} />,
  component: IssuesRoute,
})

function IssuesRoute() {
  return (
    <IssuesPage
      filters={Route.useSearch()}
      identityDid={Route.useRouteContext().identity?.did}
      params={Route.useParams()}
    />
  )
}
