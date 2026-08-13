import { HeadContent, Outlet, Scripts, createRootRouteWithContext } from '@tanstack/react-router'
import type { QueryClient } from '@tanstack/react-query'

import adenosineMarkDark from '@/assets/adenosine-mark-dark.svg?url'
import adenosineMarkLight from '@/assets/adenosine-mark-light.svg?url'
import { AppShell } from '@/components/app-shell'
import { identityQueryOptions } from '@/features/identity/identity.query'
import globalsCss from '@/styles/globals.css?url'

export type RouterContext = { queryClient: QueryClient }

export const Route = createRootRouteWithContext<RouterContext>()({
  beforeLoad: async ({ context }) => {
    const identity = await context.queryClient.ensureQueryData(identityQueryOptions())
    return { identity }
  },
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { title: 'Adenosine' },
      { name: 'description', content: 'Federated Git hosting with portable identity.' },
    ],
    links: [
      { rel: 'stylesheet', href: globalsCss },
      {
        rel: 'icon',
        type: 'image/svg+xml',
        href: adenosineMarkLight,
        media: '(prefers-color-scheme: light)',
      },
      {
        rel: 'icon',
        type: 'image/svg+xml',
        href: adenosineMarkDark,
        media: '(prefers-color-scheme: dark)',
      },
    ],
  }),
  component: RootComponent,
  shellComponent: RootDocument,
})

function RootComponent() {
  const { identity } = Route.useRouteContext()
  return (
    <AppShell identity={identity}>
      <Outlet />
    </AppShell>
  )
}

function RootDocument({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <head>
        <HeadContent />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  )
}
