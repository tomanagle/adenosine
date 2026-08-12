import { createFileRoute } from '@tanstack/react-router'

import { didSchema } from '@/features/collaboration/identity'
import { ProfilePage } from '@/features/profiles/profile-page'
import { profileQueryOptions } from '@/features/profiles/profile.query'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'

export const Route = createFileRoute('/profiles/$identity')({
  ssr: false,
  params: { parse: (params) => ({ identity: didSchema.parse(params.identity) }) },
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(profileQueryOptions(params.identity)),
  pendingComponent: RepositoryPending,
  errorComponent: ({ error }) => <RepositoryError error={error} />,
  component: ProfileRoute,
})

function ProfileRoute() {
  return <ProfilePage did={Route.useParams().identity} />
}
