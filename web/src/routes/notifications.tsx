import { createFileRoute, redirect } from '@tanstack/react-router'

import { NotificationsPage } from '@/features/notifications/notifications-page'
import { notificationsQueryOptions } from '@/features/notifications/queries'

export const Route = createFileRoute('/notifications')({
  ssr: false,
  beforeLoad: ({ context }) => {
    if (!context.identity) throw redirect({ to: '/login' })
  },
  loader: ({ context }) => context.queryClient.ensureQueryData(notificationsQueryOptions()),
  component: NotificationsPage,
})
