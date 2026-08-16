import {
  zSyncIssue,
  zSyncIssueComment,
  zSyncProfile,
  zSyncPullRequest,
  zSyncPullRequestReview,
  zSyncRepository,
  zSyncStar,
} from '@adenosine/api-client/schemas'
import { FetchError } from '@electric-sql/client'
import { BTreeIndex } from '@tanstack/db'
import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  createRouteElectricCollection,
  routeElectricConfiguration,
  routeElectricResources,
} from './route-electric'

afterEach(() => vi.unstubAllGlobals())

describe('createRouteElectricCollection', () => {
  it('rejects construction during SSR', () => {
    expect(() => createRouteElectricCollection('home', 'repositories')).toThrow('browser-only')
  })

  it('builds fixed same-origin POST resources with bounded retry behavior', () => {
    const configuration = routeElectricConfiguration(
      'home',
      'repositories',
      '/api/v1/sync/repositories',
      'http://localhost:3000',
    )

    expect(configuration).toMatchObject({
      id: 'route:home:repositories',
      syncMode: 'on-demand',
      autoIndex: 'eager',
      defaultIndexType: BTreeIndex,
      request: {
        url: 'http://localhost:3000/api/v1/sync/repositories',
        subsetMethod: 'POST',
      },
    })
    expect(
      configuration.request.onError(new FetchError(401, 'unauthorized', undefined, {}, '/sync')),
    ).toBeUndefined()
    expect(
      configuration.request.onError(new FetchError(429, 'busy', undefined, {}, '/sync')),
    ).toEqual({})
    expect(configuration.request.onError(new Error('offline'))).toEqual({})
  })
})

describe('route Electric resource mapping', () => {
  it('maps every public Sync resource to its generated schema, endpoint, and typed key', () => {
    const expected = {
      profiles: { url: '/api/v1/sync/profiles', schema: zSyncProfile },
      repositories: { url: '/api/v1/sync/repositories', schema: zSyncRepository },
      stars: { url: '/api/v1/sync/stars', schema: zSyncStar },
      issues: { url: '/api/v1/sync/issues', schema: zSyncIssue },
      'issue-comments': { url: '/api/v1/sync/issue-comments', schema: zSyncIssueComment },
      'pull-requests': { url: '/api/v1/sync/pull-requests', schema: zSyncPullRequest },
      'pull-request-reviews': {
        url: '/api/v1/sync/pull-request-reviews',
        schema: zSyncPullRequestReview,
      },
    } satisfies Record<keyof typeof routeElectricResources, { url: string; schema: object }>

    expect(routeElectricResources).toMatchObject(expected)
  })
})
