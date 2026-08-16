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
import type { ZodObject, output } from 'zod'

const electricRequestOptionsKey = 'shapeOptions' as const

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
  if (
    error instanceof FetchError &&
    error.status >= 400 &&
    error.status < 500 &&
    error.status !== 429
  )
    return
  return {}
}

export function routeElectricConfiguration(
  routeScope: string,
  resource: RouteElectricResource,
  endpoint: `/api/v1/sync/${string}`,
  origin: string,
) {
  return {
    id: `route:${routeScope}:${resource}`,
    syncMode: 'on-demand' as const,
    autoIndex: 'eager' as const,
    defaultIndexType: BTreeIndex,
    request: {
      url: new URL(endpoint, origin).toString(),
      subsetMethod: 'POST' as const,
      onError: retryTransientSyncError,
    },
  }
}

function createFixedRouteCollection<TSchema extends ZodObject>(
  routeScope: string,
  resource: RouteElectricResource,
  definition: Readonly<{
    url: `/api/v1/sync/${string}`
    schema: TSchema
    getKey: (row: output<TSchema>) => string
  }>,
) {
  const configuration = routeElectricConfiguration(
    routeScope,
    resource,
    definition.url,
    window.location.origin,
  )
  const options = electricCollectionOptions({
    id: configuration.id,
    schema: definition.schema,
    getKey: (row) => definition.getKey(definition.schema.parse(row)),
    syncMode: configuration.syncMode,
    autoIndex: configuration.autoIndex,
    defaultIndexType: configuration.defaultIndexType,
    [electricRequestOptionsKey]: configuration.request,
  })
  // SAFETY: Both libraries consume the same Standard Schema output, but expose it through
  // incompatible conditional types. Runtime rows are parsed by definition.schema above.
  return createCollection(options as never)
}

function bindCollectionResource<
  R extends RouteElectricResource,
  TCollection extends object = object,
>(collection: TCollection): RouteElectricCollection<R> {
  // SAFETY: Callers select the schema and collection using the same literal resource branch as R.
  return collection as RouteElectricCollection<R>
}

export function createRouteElectricCollection<R extends RouteElectricResource>(
  routeScope: string,
  resource: R,
): RouteElectricCollection<R> {
  if (!('window' in globalThis)) {
    throw new Error('Route Electric collections are browser-only')
  }
  if (routeScope.length === 0) throw new Error('Route Electric collections require a route scope')

  switch (resource) {
    case 'profiles':
      return bindCollectionResource<R>(
        createFixedRouteCollection(routeScope, resource, routeElectricResources.profiles),
      )
    case 'repositories':
      return bindCollectionResource<R>(
        createFixedRouteCollection(routeScope, resource, routeElectricResources.repositories),
      )
    case 'stars':
      return bindCollectionResource<R>(
        createFixedRouteCollection(routeScope, resource, routeElectricResources.stars),
      )
    case 'issues':
      return bindCollectionResource<R>(
        createFixedRouteCollection(routeScope, resource, routeElectricResources.issues),
      )
    case 'issue-comments':
      return bindCollectionResource<R>(
        createFixedRouteCollection(routeScope, resource, routeElectricResources['issue-comments']),
      )
    case 'pull-requests':
      return bindCollectionResource<R>(
        createFixedRouteCollection(routeScope, resource, routeElectricResources['pull-requests']),
      )
    case 'pull-request-reviews':
      return bindCollectionResource<R>(
        createFixedRouteCollection(
          routeScope,
          resource,
          routeElectricResources['pull-request-reviews'],
        ),
      )
  }
}

export const createProfileCollection = (routeScope: string) =>
  createRouteElectricCollection(routeScope, 'profiles')
export const createRepositoryCollection = (routeScope: string) =>
  createRouteElectricCollection(routeScope, 'repositories')
export const createStarCollection = (routeScope: string) =>
  createRouteElectricCollection(routeScope, 'stars')
export const createIssueCollection = (routeScope: string) =>
  createRouteElectricCollection(routeScope, 'issues')
export const createIssueCommentCollection = (routeScope: string) =>
  createRouteElectricCollection(routeScope, 'issue-comments')
export const createPullRequestCollection = (routeScope: string) =>
  createRouteElectricCollection(routeScope, 'pull-requests')
export const createPullRequestReviewCollection = (routeScope: string) =>
  createRouteElectricCollection(routeScope, 'pull-request-reviews')
