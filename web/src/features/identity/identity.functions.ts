import { createClient, getCurrentIdentity } from '@adenosine/api-client'
import { createServerFn } from '@tanstack/react-start'
import { getRequestHeader } from '@tanstack/react-start/server'

import { classifyIdentityResult } from './identity'

export const getIdentity = createServerFn({ method: 'GET' }).handler(async () => {
  const cookie = getRequestHeader('cookie')
  const client = createClient({
    baseUrl: process.env.ADENOSINE_INTERNAL_API_URL ?? 'http://localhost:8080',
    headers: cookie ? { cookie } : undefined,
  })
  const result = await getCurrentIdentity({ client })

  return classifyIdentityResult(result)
})
