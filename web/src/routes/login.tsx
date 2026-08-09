import { createFileRoute, redirect } from '@tanstack/react-router'

import { identityQueryOptions } from '@/features/identity/identity.query'
import { LoginPage } from '@/features/login/login-page'

export const Route = createFileRoute('/login')({
  ssr: false,
  loader: async ({ context }) => {
    const identity = await context.queryClient.ensureQueryData(identityQueryOptions())
    if (identity) throw redirect({ to: '/home' })
  },
  component: LoginPage,
})
