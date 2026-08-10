import { createFileRoute } from '@tanstack/react-router'

import { HomePage } from '@/features/home/home-page'
import { LandingPage } from '@/features/landing/landing-page'

export const Route = createFileRoute('/')({
  component: IndexPage,
})

function IndexPage() {
  const { identity } = Route.useRouteContext()
  return identity ? <HomePage /> : <LandingPage />
}
