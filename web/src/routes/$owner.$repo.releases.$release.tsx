import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'

import { ReleaseDetailPage } from '@/features/repository-browser/releases'
import {
  releaseAssetsQueryOptions,
  releaseQueryOptions,
  releasesQueryOptions,
} from '@/features/repository-browser/queries'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'

const paramsSchema = z.object({ release: z.string().uuid() })

export const Route = createFileRoute('/$owner/$repo/releases/$release')({
  ssr: false,
  params: { parse: (params) => ({ ...params, ...paramsSchema.parse(params) }) },
  loader: async ({ context, params }) => {
    await Promise.all([
      context.queryClient.ensureQueryData(releaseQueryOptions(params, params.release)),
      context.queryClient.ensureQueryData(releaseAssetsQueryOptions(params, params.release)),
      context.queryClient.ensureQueryData(releasesQueryOptions(params)),
    ])
  },
  pendingComponent: RepositoryPending,
  errorComponent: ({ error }) => <RepositoryError error={error} />,
  component: ReleaseRoute,
})

function ReleaseRoute() {
  const { release, ...params } = Route.useParams()
  return <ReleaseDetailPage params={params} releaseId={release} />
}
