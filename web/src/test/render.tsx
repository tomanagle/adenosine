import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryHistory, RouterContextProvider } from '@tanstack/react-router'
import { render } from '@testing-library/react'
import type { ReactNode } from 'react'

import { getRouter } from '@/router'

export function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  })
}

export function renderWithAppProviders(
  children: ReactNode,
  {
    initialEntry = '/',
    queryClient = createTestQueryClient(),
  }: { initialEntry?: string; queryClient?: QueryClient } = {},
) {
  const router = getRouter({
    history: createMemoryHistory({ initialEntries: [initialEntry] }),
    queryClient,
  })

  return {
    queryClient,
    router,
    ...render(
      <QueryClientProvider client={queryClient}>
        <RouterContextProvider router={router}>{children}</RouterContextProvider>
      </QueryClientProvider>,
    ),
  }
}
