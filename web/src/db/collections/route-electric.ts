import {
  zSyncIssue,
  zSyncIssueComment,
  zSyncProfile,
  zSyncPullRequest,
  zSyncPullRequestReview,
  zSyncRepository,
  zSyncStar,
} from '@adenosine/api-client/schemas'
import type {
  SyncIssueCommentRow,
  SyncIssueRow,
  SyncProfileRow,
  SyncPullRequestReviewRow,
  SyncPullRequestRow,
  SyncRepositoryRow,
  SyncStarRow,
} from '@adenosine/api-client/schemas'
import { FetchError } from '@electric-sql/client'
import { BTreeIndex, createCollection } from '@tanstack/db'
import type { Collection } from '@tanstack/db'
import { electricCollectionOptions } from '@tanstack/electric-db-collection'
import type { ElectricCollectionUtils } from '@tanstack/electric-db-collection'
import type { z } from 'zod'

export type RouteElectricResource =
  | 'profiles'
  | 'repositories'
  | 'stars'
  | 'issues'
  | 'issue-comments'
  | 'pull-requests'
  | 'pull-request-reviews'

export type RouteElectricRowMap = {
  profiles: SyncProfileRow
  repositories: SyncRepositoryRow
  stars: SyncStarRow
  issues: SyncIssueRow
  'issue-comments': SyncIssueCommentRow
  'pull-requests': SyncPullRequestRow
  'pull-request-reviews': SyncPullRequestReviewRow
}

// These are the only browser-selectable sync resources. The server retains
// ownership of tables, columns, and its visibility/moderation predicate.
export const routeElectricResources = Object.freeze({
  profiles: Object.freeze({
    url: '/api/v1/sync/profiles',
    schema: zSyncProfile,
    getKey: (profile: SyncProfileRow) => profile.did,
  }),
  repositories: Object.freeze({
    url: '/api/v1/sync/repositories',
    schema: zSyncRepository,
    getKey: (repository: SyncRepositoryRow) => repository.uri,
  }),
  stars: Object.freeze({
    url: '/api/v1/sync/stars',
    schema: zSyncStar,
    getKey: (star: SyncStarRow) => star.uri,
  }),
  issues: Object.freeze({
    url: '/api/v1/sync/issues',
    schema: zSyncIssue,
    getKey: (issue: SyncIssueRow) => issue.uri,
  }),
  'issue-comments': Object.freeze({
    url: '/api/v1/sync/issue-comments',
    schema: zSyncIssueComment,
    getKey: (comment: SyncIssueCommentRow) => comment.uri,
  }),
  'pull-requests': Object.freeze({
    url: '/api/v1/sync/pull-requests',
    schema: zSyncPullRequest,
    getKey: (pullRequest: SyncPullRequestRow) => pullRequest.uri,
  }),
  'pull-request-reviews': Object.freeze({
    url: '/api/v1/sync/pull-request-reviews',
    schema: zSyncPullRequestReview,
    getKey: (review: SyncPullRequestReviewRow) => review.uri,
  }),
})

export type RouteElectricCollection<R extends RouteElectricResource> = Collection<
  RouteElectricRowMap[R],
  string,
  ElectricCollectionUtils<RouteElectricRowMap[R]>
>

function retryTransientSyncError(error: Error) {
  if (error instanceof FetchError && error.status >= 400 && error.status < 500 && error.status !== 429) return
  return {}
}

function createFixedRouteCollection<TShape extends z.ZodRawShape>(
  routeScope: string,
  resource: RouteElectricResource,
  definition: Readonly<{
    url: `/api/v1/sync/${string}`
    schema: z.ZodObject<TShape>
    getKey: (row: z.output<z.ZodObject<TShape>>) => string
  }>,
) {
  const options = electricCollectionOptions({
    id: `route:${routeScope}:${resource}`,
    schema: definition.schema,
    getKey: (row: Record<string, unknown>) => definition.getKey(row as unknown as z.output<z.ZodObject<TShape>>),
    syncMode: 'on-demand',
    autoIndex: 'eager',
    defaultIndexType: BTreeIndex,
    shapeOptions: {
      url: new URL(definition.url, window.location.origin).toString(),
      subsetMethod: 'POST',
      onError: retryTransientSyncError,
    },
  })
  // The adapter and DB package infer the same schema output through separate
  // conditional types that TypeScript cannot reconcile while TShape is generic.
  return createCollection(options as never)
}

export function createRouteElectricCollection<R extends RouteElectricResource>(
  routeScope: string,
  resource: R,
): RouteElectricCollection<R> {
  if (typeof window === 'undefined') {
    throw new Error('Route Electric collections are browser-only')
  }
  if (routeScope.length === 0) throw new Error('Route Electric collections require a route scope')

  let collection
  switch (resource) {
    case 'profiles':
      collection = createFixedRouteCollection(routeScope, resource, routeElectricResources.profiles)
      break
    case 'repositories':
      collection = createFixedRouteCollection(routeScope, resource, routeElectricResources.repositories)
      break
    case 'stars':
      collection = createFixedRouteCollection(routeScope, resource, routeElectricResources.stars)
      break
    case 'issues':
      collection = createFixedRouteCollection(routeScope, resource, routeElectricResources.issues)
      break
    case 'issue-comments':
      collection = createFixedRouteCollection(routeScope, resource, routeElectricResources['issue-comments'])
      break
    case 'pull-requests':
      collection = createFixedRouteCollection(routeScope, resource, routeElectricResources['pull-requests'])
      break
    case 'pull-request-reviews':
      collection = createFixedRouteCollection(routeScope, resource, routeElectricResources['pull-request-reviews'])
      break
  }
  return collection as unknown as RouteElectricCollection<R>
}

export const createProfileCollection = (routeScope: string) => createRouteElectricCollection(routeScope, 'profiles')
export const createRepositoryCollection = (routeScope: string) => createRouteElectricCollection(routeScope, 'repositories')
export const createStarCollection = (routeScope: string) => createRouteElectricCollection(routeScope, 'stars')
export const createIssueCollection = (routeScope: string) => createRouteElectricCollection(routeScope, 'issues')
export const createIssueCommentCollection = (routeScope: string) => createRouteElectricCollection(routeScope, 'issue-comments')
export const createPullRequestCollection = (routeScope: string) => createRouteElectricCollection(routeScope, 'pull-requests')
export const createPullRequestReviewCollection = (routeScope: string) => createRouteElectricCollection(routeScope, 'pull-request-reviews')
