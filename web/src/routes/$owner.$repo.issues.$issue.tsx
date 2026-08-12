import { createFileRoute } from '@tanstack/react-router'

import { decodeRecordIdentity } from '@/features/collaboration/identity'
import { IssuePage } from '@/features/collaboration/pages'
import { commentsQueryOptions, issueQueryOptions } from '@/features/collaboration/queries'
import { repositoryQueryOptions } from '@/features/repository-browser/queries'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'

export const Route = createFileRoute('/$owner/$repo/issues/$issue')({
  ssr: false,
  params: { parse: (params) => ({ ...params, issue: decodeRecordIdentity(params.issue) }) },
  loader: async ({ context, params }) => {
    const repository = await context.queryClient.ensureQueryData(repositoryQueryOptions(params))
    await Promise.all([
      context.queryClient.ensureQueryData(issueQueryOptions(repository.uri ?? '', params.issue)),
      context.queryClient.ensureQueryData(commentsQueryOptions(params.issue)),
    ])
  },
  pendingComponent: RepositoryPending,
  errorComponent: ({ error }) => <RepositoryError error={error} />,
  component: IssueRoute,
})

function IssueRoute() {
  const params = Route.useParams()
  return (
    <IssuePage
      identityDid={Route.useRouteContext().identity?.did}
      issueUri={params.issue}
      params={params}
    />
  )
}
