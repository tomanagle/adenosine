import type { QueryClient } from '@tanstack/react-query'

import {
  branchesQueryOptions,
  blobQueryOptions,
  commitQueryOptions,
  commitsQueryOptions,
  diffQueryOptions,
  mergeBaseQueryOptions,
  repositoryQueryOptions,
  starsQueryOptions,
  tagsQueryOptions,
  treeQueryOptions,
} from './queries'
import type { RepositoryRouteParams } from './queries'
import { classifyBrowserError } from './states'
import { findReadme } from './view-models'
import { splitBlobPath } from './validation'

const MAX_RENDERED_BLOB = 2 * 1024 * 1024
const MAX_README = 512 * 1024

export async function loadRepository(queryClient: QueryClient, params: RepositoryRouteParams) {
  const repository = await queryClient.ensureQueryData(repositoryQueryOptions(params))
  if (repository.hosting.source_browsing !== 'local') return repository
  await Promise.allSettled([
    queryClient.ensureQueryData(branchesQueryOptions(params)),
    queryClient.ensureQueryData(tagsQueryOptions(params)),
    ...(repository.uri ? [queryClient.ensureQueryData(starsQueryOptions(repository.uri))] : []),
  ])
  return repository
}

export async function loadOverview(queryClient: QueryClient, params: RepositoryRouteParams) {
  const repository = await loadRepository(queryClient, params)
  if (repository.hosting.source_browsing !== 'local') return
  const tree = await queryClient
    .ensureQueryData(treeQueryOptions(params, repository.default_branch))
    .catch((error: unknown) => {
      if (classifyBrowserError(error) === 'missing') return undefined
      throw error
    })
  if (!tree) return
  const readme = findReadme(tree.entries)
  if (readme?.size != null && readme.size <= MAX_README) {
    await queryClient.ensureQueryData(blobQueryOptions(params, readme.sha))
  }
}

export async function loadTree(
  queryClient: QueryClient,
  params: RepositoryRouteParams,
  ref: string | undefined,
  treePath: string,
) {
  const repository = await loadRepository(queryClient, params)
  if (repository.hosting.source_browsing !== 'local') return
  await queryClient.ensureQueryData(
    treeQueryOptions(params, ref ?? repository.default_branch, treePath),
  )
}

export async function loadBlob(
  queryClient: QueryClient,
  params: RepositoryRouteParams,
  ref: string | undefined,
  routePath: string,
) {
  const repository = await loadRepository(queryClient, params)
  if (repository.hosting.source_browsing !== 'local') return
  const file = splitBlobPath(routePath)
  const revision = ref ?? repository.default_branch
  const tree = await queryClient.ensureQueryData(
    treeQueryOptions(params, revision, file.parentPath),
  )
  const entry = tree.entries.find((candidate) => candidate.name === file.name)
  if (!entry || entry.type !== 'blob') throw new Error('Repository file not found')
  if (entry.size != null && entry.size <= MAX_RENDERED_BLOB) {
    await queryClient.ensureQueryData(blobQueryOptions(params, entry.sha))
  }
}

export async function loadCommits(
  queryClient: QueryClient,
  params: RepositoryRouteParams,
  ref: string | undefined,
  limit: number,
) {
  const repository = await loadRepository(queryClient, params)
  if (repository.hosting.source_browsing !== 'local') return
  await queryClient.ensureQueryData(
    commitsQueryOptions(params, ref ?? repository.default_branch, limit),
  )
}

export async function loadCommit(
  queryClient: QueryClient,
  params: RepositoryRouteParams,
  revision: string,
) {
  const repository = await loadRepository(queryClient, params)
  if (repository.hosting.source_browsing !== 'local') return
  const commit = await queryClient.ensureQueryData(commitQueryOptions(params, revision))
  if (commit.parents[0]) {
    await queryClient
      .ensureQueryData(diffQueryOptions(params, commit.parents[0], commit.sha))
      .catch(() => undefined)
  }
}

export async function loadComparison(
  queryClient: QueryClient,
  params: RepositoryRouteParams,
  base: string | undefined,
  head: string | undefined,
) {
  const repository = await loadRepository(queryClient, params)
  if (repository.hosting.source_browsing !== 'local') return
  if (base && head) {
    await Promise.allSettled([
      queryClient.ensureQueryData(diffQueryOptions(params, base, head)),
      queryClient.ensureQueryData(mergeBaseQueryOptions(params, base, head)),
    ])
  }
}

export { MAX_README, MAX_RENDERED_BLOB }
