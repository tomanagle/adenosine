import { QueryClient } from '@tanstack/react-query'
import { createRouter } from '@tanstack/react-router'
import type { RouterHistory } from '@tanstack/react-router'
import { setupRouterSsrQueryIntegration } from '@tanstack/react-router-ssr-query'

import { routeTree } from './routeTree.gen'

export function getRouter({
  history,
  queryClient = new QueryClient({
    defaultOptions: {
      queries: { staleTime: 10_000, retry: 1 },
    },
  }),
}: { history?: RouterHistory; queryClient?: QueryClient } = {}) {
  const router = createRouter({
    routeTree,
    context: { queryClient },
    defaultPreload: 'intent',
    history,
    scrollRestoration: true,
  })

  setupRouterSsrQueryIntegration({ router, queryClient })
  return router
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof getRouter>
  }
}
