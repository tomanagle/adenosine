import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'

import { RemoteSourceState } from '@/features/repository-browser/code-browser'
import { CommitHistory } from '@/features/repository-browser/history'
import { loadCommits } from '@/features/repository-browser/loaders'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'
import { repositoryQueryOptions } from '@/features/repository-browser/queries'
import { commitsSearchSchema } from '@/features/repository-browser/validation'

export const Route = createFileRoute('/$owner/$repo/commits')({
  ssr: false,
  validateSearch: commitsSearchSchema,
  loaderDeps: ({ search }) => search,
  loader: ({ context, params, deps }) =>
    loadCommits(context.queryClient, params, deps.ref, deps.limit),
  pendingComponent: RepositoryPending,
  errorComponent: ({ error }) => <RepositoryError error={error} />,
  component: CommitsRoute,
})

function CommitsRoute() {
  const search = Route.useSearch()
  const params = Route.useParams()
  const { data: repository } = useSuspenseQuery(repositoryQueryOptions(params))
  if (repository.hosting.source_browsing !== 'local')
    return <RemoteSourceState webUrl={repository.hosting.web_url} />
  return <CommitHistory params={params} ref={search.ref} limit={search.limit} />
}
