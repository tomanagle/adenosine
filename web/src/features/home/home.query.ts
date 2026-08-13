import { createPullRequestMutation, createRepositoryMutation } from '@adenosine/api-client/query'

import { browserApiClient } from '@/api/browser-client'

export const createRepositoryMutationOptions = () =>
  createRepositoryMutation({ client: browserApiClient })

export const createPullRequestMutationOptions = () =>
  createPullRequestMutation({ client: browserApiClient })
