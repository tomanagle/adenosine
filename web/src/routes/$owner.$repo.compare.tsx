import { createFileRoute } from '@tanstack/react-router'

import { loadComparison } from '@/features/repository-browser/loaders'
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
  return (
    <CompareView
      key={`${search.base ?? ''}\u0000${search.head ?? ''}`}
      params={Route.useParams()}
      base={search.base}
      head={search.head}
    />
  )
}
