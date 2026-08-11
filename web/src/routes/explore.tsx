import { createFileRoute } from '@tanstack/react-router'

import { ExploreError, ExplorePage, ExplorePending } from '@/features/explore/explore-page'
import {
  profileSearchQueryOptions,
  repositorySearchQueryOptions,
} from '@/features/explore/explore.query'
import { parseExploreSearch } from '@/features/explore/explore-search'

export const Route = createFileRoute('/explore')({
  validateSearch: parseExploreSearch,
  loaderDeps: ({ search }) => search,
  loader: async ({ context, deps }) => {
    if (!deps.q) return
    if (deps.type === 'repositories') {
      await context.queryClient.ensureQueryData(repositorySearchQueryOptions(deps))
    } else {
      await context.queryClient.ensureQueryData(profileSearchQueryOptions(deps))
    }
  },
  pendingComponent: ExplorePending,
  errorComponent: ({ reset }) => <ExploreError reset={reset} />,
  component: ExploreRoute,
})

function ExploreRoute() {
  return <ExplorePage search={Route.useSearch()} />
}
