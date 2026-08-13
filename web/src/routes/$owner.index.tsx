import { useSuspenseQuery } from '@tanstack/react-query'
import { createFileRoute, redirect } from '@tanstack/react-router'
import { z } from 'zod'

import { OrganizationPage } from '@/features/organizations/organization-page'
import {
  organizationMembersInfiniteQueryOptions,
  organizationQueryOptions,
  organizationTeamsInfiniteQueryOptions,
} from '@/features/organizations/queries'
import { ownerQueryOptions } from '@/features/owners/queries'
import { ProfilePage } from '@/features/profiles/profile-page'
import { profileQueryOptions } from '@/features/profiles/profile.query'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'

const ownerNameSchema = z
  .string()
  .regex(/^[a-z0-9][a-z0-9.-]*$/i)
  .max(253)

export const Route = createFileRoute('/$owner/')({
  ssr: false,
  loader: async ({ context, params }) => {
    const name = ownerNameSchema.parse(params.owner)
    const resolved = await context.queryClient.ensureQueryData(ownerQueryOptions(name))
    if (resolved.canonical_name !== name) {
      throw redirect({ to: '/$owner', params: { owner: resolved.canonical_name }, replace: true })
    }
    if (resolved.kind === 'account') {
      if (!resolved.account_did) throw new Error('The account owner is missing its DID.')
      await context.queryClient.ensureQueryData(profileQueryOptions(resolved.account_did))
      return resolved
    }
    if (!resolved.organization_slug) {
      throw new Error('The organization owner is missing its slug.')
    }
    await Promise.all([
      context.queryClient.ensureQueryData(organizationQueryOptions(resolved.organization_slug)),
      context.queryClient.ensureInfiniteQueryData(
        organizationMembersInfiniteQueryOptions(resolved.organization_slug),
      ),
      ...(context.identity
        ? [
            context.queryClient.ensureInfiniteQueryData(
              organizationTeamsInfiniteQueryOptions(resolved.organization_slug),
            ),
          ]
        : []),
    ])
    return resolved
  },
  pendingComponent: RepositoryPending,
  errorComponent: ({ error }) => <RepositoryError error={error} />,
  component: OwnerRoute,
})

function OwnerRoute() {
  const { identity } = Route.useRouteContext()
  const name = ownerNameSchema.parse(Route.useParams().owner)
  const { data: resolved } = useSuspenseQuery(ownerQueryOptions(name))
  if (resolved.kind === 'account' && resolved.account_did) {
    return <ProfilePage did={resolved.account_did} />
  }
  if (resolved.kind === 'organization' && resolved.organization_slug) {
    return <OrganizationPage identity={identity} slug={resolved.organization_slug} />
  }
  throw new Error('The owner response does not identify a resource.')
}
