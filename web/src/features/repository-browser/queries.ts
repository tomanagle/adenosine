import {
  deleteStarMutation,
  createRepositoryForkMutation,
  getRepositoryBlobOptions,
  getRepositoryCommitOptions,
  getRepositoryDiffOptions,
  getRepositoryMergeBaseOptions,
  getRepositoryOptions,
  getRepositoryTreeOptions,
  getStarsOptions,
  listRepositoryForksOptions,
  listRepositoryBranchesOptions,
  listRepositoryCommitsOptions,
  listRepositoryTagsOptions,
  putStarMutation,
  syncRepositoryForkMutation,
  updateRepositoryMutation,
  deleteRepositoryMutation,
  restoreRepositoryDeletionMutation,
  listRepositoryWebhooksOptions,
  createRepositoryWebhookMutation,
  deleteRepositoryWebhookMutation,
  listBranchProtectionsOptions,
  createBranchProtectionMutation,
  deleteBranchProtectionMutation,
} from '@adenosine/api-client/query'

import { browserApiClient } from '@/api/browser-client'

export type RepositoryRouteParams = { owner: string; repo: string }

const path = ({ owner, repo }: RepositoryRouteParams) => ({ owner, repo })

export const repositoryQueryOptions = (params: RepositoryRouteParams) =>
  getRepositoryOptions({ client: browserApiClient, path: path(params) })

export const starsQueryOptions = (repositoryUri: string) =>
  getStarsOptions({ client: browserApiClient, query: { repository_uri: repositoryUri } })

export const putStarMutationOptions = () => putStarMutation({ client: browserApiClient })

export const deleteStarMutationOptions = () => deleteStarMutation({ client: browserApiClient })

export const repositoryForksQueryOptions = (params: RepositoryRouteParams) =>
  listRepositoryForksOptions({ client: browserApiClient, path: path(params) })

export const createRepositoryForkMutationOptions = () =>
  createRepositoryForkMutation({ client: browserApiClient })

export const syncRepositoryForkMutationOptions = () =>
  syncRepositoryForkMutation({ client: browserApiClient })

export const updateRepositoryMutationOptions = () =>
  updateRepositoryMutation({ client: browserApiClient })
export const deleteRepositoryMutationOptions = () =>
  deleteRepositoryMutation({ client: browserApiClient })
export const restoreRepositoryDeletionMutationOptions = () =>
  restoreRepositoryDeletionMutation({ client: browserApiClient })
export const repositoryWebhooksQueryOptions = (params: RepositoryRouteParams) =>
  listRepositoryWebhooksOptions({
    client: browserApiClient,
    path: path(params),
    query: { limit: 100 },
  })
export const createRepositoryWebhookMutationOptions = () =>
  createRepositoryWebhookMutation({ client: browserApiClient })
export const deleteRepositoryWebhookMutationOptions = () =>
  deleteRepositoryWebhookMutation({ client: browserApiClient })
export const branchProtectionsQueryOptions = (params: RepositoryRouteParams) =>
  listBranchProtectionsOptions({
    client: browserApiClient,
    path: path(params),
    query: { limit: 100 },
  })
export const createBranchProtectionMutationOptions = () =>
  createBranchProtectionMutation({ client: browserApiClient })
export const deleteBranchProtectionMutationOptions = () =>
  deleteBranchProtectionMutation({ client: browserApiClient })

export const branchesQueryOptions = (params: RepositoryRouteParams) => ({
  ...listRepositoryBranchesOptions({ client: browserApiClient, path: path(params) }),
  retryOnMount: false,
})

export const tagsQueryOptions = (params: RepositoryRouteParams) => ({
  ...listRepositoryTagsOptions({ client: browserApiClient, path: path(params) }),
  retryOnMount: false,
})

export const treeQueryOptions = (params: RepositoryRouteParams, revision: string, treePath = '') =>
  getRepositoryTreeOptions({
    client: browserApiClient,
    path: path(params),
    query: { rev: revision, path: treePath || undefined },
  })

export const blobQueryOptions = (params: RepositoryRouteParams, sha: string) =>
  getRepositoryBlobOptions({
    client: browserApiClient,
    path: { ...path(params), sha },
  })

export const commitsQueryOptions = (params: RepositoryRouteParams, ref: string, limit: number) =>
  listRepositoryCommitsOptions({
    client: browserApiClient,
    path: path(params),
    query: { ref, limit },
  })

export const commitQueryOptions = (params: RepositoryRouteParams, revision: string) =>
  getRepositoryCommitOptions({
    client: browserApiClient,
    path: { ...path(params), revision },
  })

export const diffQueryOptions = (params: RepositoryRouteParams, base: string, head: string) => ({
  ...getRepositoryDiffOptions({
    client: browserApiClient,
    path: path(params),
    query: { base, head },
  }),
  retryOnMount: false,
})

export const mergeBaseQueryOptions = (
  params: RepositoryRouteParams,
  first: string,
  second: string,
) => ({
  ...getRepositoryMergeBaseOptions({
    client: browserApiClient,
    path: path(params),
    query: { a: first, b: second },
  }),
  retryOnMount: false,
})
