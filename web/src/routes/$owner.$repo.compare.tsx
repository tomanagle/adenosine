import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'

import { RemoteSourceState } from '@/features/repository-browser/code-browser'
import { loadComparison } from '@/features/repository-browser/loaders'
import { repositoryQueryOptions } from '@/features/repository-browser/queries'
import { CompareView } from '@/features/repository-browser/repository-diff'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'
import { compareSearchSchema } from '@/features/repository-browser/validation'

export const Route = createFileRoute('/$owner/$repo/compare')({
  ssr: false,
  validateSearch: compareSearchSchema,
  loaderDeps: ({ search }) => search,
  loader: ({ context, params, deps }) =>
    loadComparison(context.queryClient, params, deps.base, deps.head),
  pendingComponent: RepositoryPending,
  errorComponent: ({ error }) => <RepositoryError error={error} />,
  component: CompareRoute,
})

function CompareRoute() {
  const search = Route.useSearch()
  const params = Route.useParams()
  const { data: repository } = useSuspenseQuery(repositoryQueryOptions(params))
  if (repository.hosting.source_browsing !== 'local')
    return <RemoteSourceState webUrl={repository.hosting.web_url} />
  return (
    <CompareView
      key={`${search.base ?? ''}\u0000${search.head ?? ''}`}
      params={params}
      base={search.base}
      head={search.head}
    />
  )
}
