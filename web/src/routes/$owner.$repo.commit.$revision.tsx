import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'

import { RemoteSourceState } from '@/features/repository-browser/code-browser'
import { CommitDetail } from '@/features/repository-browser/history'
import { loadCommit } from '@/features/repository-browser/loaders'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'
import { repositoryQueryOptions } from '@/features/repository-browser/queries'
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
  const { data: repository } = useSuspenseQuery(repositoryQueryOptions(params))
  if (repository.hosting.source_browsing !== 'local')
    return <RemoteSourceState webUrl={repository.hosting.web_url} />
  return <CommitDetail params={params} revision={params.revision} />
}
