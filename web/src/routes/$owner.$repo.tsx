import { createFileRoute } from '@tanstack/react-router'

import { loadRepository } from '@/features/repository-browser/loaders'
import { RepositoryLayout } from '@/features/repository-browser/repository-layout'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'
import { repositoryParamsSchema } from '@/features/repository-browser/validation'

export const Route = createFileRoute('/$owner/$repo')({
  ssr: false,
  params: { parse: (params) => repositoryParamsSchema.parse(params) },
  loader: ({ context, params }) => loadRepository(context.queryClient, params),
  pendingComponent: RepositoryPending,
  errorComponent: ({ error }) => <RepositoryError error={error} />,
  component: RepositoryRoute,
})

function RepositoryRoute() {
  const { identity } = Route.useRouteContext()
  return <RepositoryLayout identityDid={identity?.did} params={Route.useParams()} />
}
