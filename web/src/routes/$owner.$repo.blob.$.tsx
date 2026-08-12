import { createFileRoute } from '@tanstack/react-router'

import { BlobBrowser } from '@/features/repository-browser/code-browser'
import { loadBlob } from '@/features/repository-browser/loaders'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'
import { parseRepositoryPath, treeSearchSchema } from '@/features/repository-browser/validation'

export const Route = createFileRoute('/$owner/$repo/blob/$')({
  ssr: false,
  validateSearch: treeSearchSchema,
  loaderDeps: ({ search }) => search,
  loader: ({ context, params, deps }) =>
    loadBlob(context.queryClient, params, deps.ref, parseRepositoryPath(params['_splat'])),
  pendingComponent: RepositoryPending,
  errorComponent: ({ error }) => <RepositoryError error={error} />,
  component: BlobRoute,
})

function BlobRoute() {
  const params = Route.useParams()
  const search = Route.useSearch()
  return (
    <BlobBrowser
      params={params}
      routePath={parseRepositoryPath(params['_splat'])}
      ref={search.ref}
    />
  )
}
