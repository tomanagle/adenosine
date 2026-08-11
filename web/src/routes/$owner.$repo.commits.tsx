import { createFileRoute } from '@tanstack/react-router'

import { CommitHistory } from '@/features/repository-browser/history'
import { loadCommits } from '@/features/repository-browser/loaders'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'
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
  return <CommitHistory params={Route.useParams()} ref={search.ref} limit={search.limit} />
}
