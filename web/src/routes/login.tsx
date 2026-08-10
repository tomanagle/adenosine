import { createFileRoute, redirect } from '@tanstack/react-router'

import { LoginPage } from '@/features/login/login-page'

export const Route = createFileRoute('/login')({
  ssr: false,
  beforeLoad: ({ context }) => {
    if (context.identity) throw redirect({ to: '/' })
  },
  component: LoginPage,
})
