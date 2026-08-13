import { createFileRoute, redirect } from '@tanstack/react-router'

import { OrganizationsPage } from '@/features/organizations/organizations-page'
import {
  organizationInvitationsInfiniteQueryOptions,
  organizationsInfiniteQueryOptions,
} from '@/features/organizations/queries'
import { RepositoryError, RepositoryPending } from '@/features/repository-browser/states'

export const Route = createFileRoute('/organizations')({
  ssr: false,
  beforeLoad: ({ context }) => {
    if (!context.identity) throw redirect({ to: '/login' })
  },
  loader: ({ context }) =>
    Promise.all([
      context.queryClient.ensureInfiniteQueryData(organizationsInfiniteQueryOptions()),
      context.queryClient.ensureInfiniteQueryData(organizationInvitationsInfiniteQueryOptions()),
    ]),
  pendingComponent: RepositoryPending,
  errorComponent: ({ error }) => <RepositoryError error={error} />,
  component: OrganizationsPage,
})
