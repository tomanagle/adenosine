import { createFileRoute } from '@tanstack/react-router'

import { TreeBrowser } from '@/features/repository-browser/code-browser'
import { loadTree } from '@/features/repository-browser/loaders'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'
import { parseRepositoryPath, treeSearchSchema } from '@/features/repository-browser/validation'

export const Route = createFileRoute('/$owner/$repo/tree/$')({
  ssr: false,
  validateSearch: treeSearchSchema,
  loaderDeps: ({ search }) => search,
  loader: ({ context, params, deps }) =>
    loadTree(context.queryClient, params, deps.ref, parseRepositoryPath(params['_splat'])),
  pendingComponent: RepositoryPending,
  errorComponent: ({ error }) => <RepositoryError error={error} />,
  component: TreeRoute,
})

function TreeRoute() {
  const params = Route.useParams()
  const search = Route.useSearch()
  return (
    <TreeBrowser params={params} path={parseRepositoryPath(params['_splat'])} ref={search.ref} />
  )
}
