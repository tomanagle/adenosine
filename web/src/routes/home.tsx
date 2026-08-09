import { createFileRoute, redirect } from '@tanstack/react-router'

import { HomePage } from '@/features/home/home-page'
import { identityQueryOptions } from '@/features/identity/identity.query'
import { repositorySnapshotQueryOptions } from '@/features/repositories/repository-snapshot.query'

export const Route = createFileRoute('/home')({
  ssr: false,
  loader: async ({ context }) => {
    const identity = await context.queryClient.ensureQueryData(identityQueryOptions())
    if (!identity) throw redirect({ to: '/login' })
    await context.queryClient.ensureQueryData(repositorySnapshotQueryOptions())
  },
  component: HomePage,
})
