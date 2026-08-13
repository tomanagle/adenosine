import type { NotificationList } from '@adenosine/api-client'
import {
  deleteNotificationMutation,
  listNotificationsInfiniteOptions,
  listNotificationsOptions,
  updateNotificationMutation,
} from '@adenosine/api-client/query'

import { browserApiClient } from '@/api/browser-client'

export const notificationsQueryOptions = (unread = false) =>
  listNotificationsOptions({ client: browserApiClient, query: { limit: 30, unread } })

export const notificationsInfiniteQueryOptions = (unread = false) => ({
  ...listNotificationsInfiniteOptions({ client: browserApiClient, query: { limit: 30, unread } }),
  initialPageParam: '',
  getNextPageParam: (page: NotificationList) => page.page.next_cursor ?? undefined,
})

export const updateNotificationMutationOptions = () =>
  updateNotificationMutation({ client: browserApiClient })
export const deleteNotificationMutationOptions = () =>
  deleteNotificationMutation({ client: browserApiClient })
