import { createClient } from '@adenosine/api-client'

export const browserApiClient = createClient({
  baseUrl: '/',
  credentials: 'include',
})
