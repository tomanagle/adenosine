import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'

import { OrganizationPage } from '@/features/organizations/organization-page'
import {
  organizationMembersInfiniteQueryOptions,
  organizationQueryOptions,
  organizationTeamsInfiniteQueryOptions,
} from '@/features/organizations/queries'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'

const slugSchema = z
  .string()
  .regex(/^[a-z0-9][a-z0-9-]*$/)
  .max(100)

export const Route = createFileRoute('/organizations/$organization')({
  ssr: false,
  params: { parse: (params) => ({ organization: slugSchema.parse(params.organization) }) },
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(organizationQueryOptions(params.organization)),
      context.queryClient.ensureInfiniteQueryData(
        organizationMembersInfiniteQueryOptions(params.organization),
      ),
      ...(context.identity
        ? [
            context.queryClient.ensureInfiniteQueryData(
              organizationTeamsInfiniteQueryOptions(params.organization),
            ),
          ]
        : []),
    ]),
  pendingComponent: RepositoryPending,
  errorComponent: ({ error }) => <RepositoryError error={error} />,
  component: OrganizationRoute,
})

function OrganizationRoute() {
  const { identity } = Route.useRouteContext()
  return <OrganizationPage identity={identity} slug={Route.useParams().organization} />
}
