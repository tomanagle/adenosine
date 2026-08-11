import { createFileRoute } from '@tanstack/react-router'

import { CommitDetail } from '@/features/repository-browser/history'
import { loadCommit } from '@/features/repository-browser/loaders'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'
import { repositoryRevisionParamsSchema } from '@/features/repository-browser/validation'

export const Route = createFileRoute('/$owner/$repo/commit/$revision')({
  ssr: false,
  params: { parse: (params) => repositoryRevisionParamsSchema.parse(params) },
  loader: ({ context, params }) => loadCommit(context.queryClient, params, params.revision),
  pendingComponent: RepositoryPending,
  errorComponent: ({ error }) => <RepositoryError error={error} />,
  component: CommitRoute,
})

function CommitRoute() {
  const params = Route.useParams()
  return <CommitDetail params={params} revision={params.revision} />
}
