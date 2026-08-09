import { createFileRoute, redirect } from '@tanstack/react-router'

import { getLandingIdentity } from '@/features/identity/identity.functions'
import { LandingPage } from '@/features/landing/landing-page'

export const Route = createFileRoute('/')({
  loader: async () => {
    const identity = await getLandingIdentity()
    if (identity) throw redirect({ to: '/home' })
  },
  component: LandingPage,
})
