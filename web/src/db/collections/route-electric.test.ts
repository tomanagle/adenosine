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
import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  createRouteElectricCollection,
  routeElectricResources,
  type RouteElectricResource,
} from './route-electric'

const adapter = vi.hoisted(() => ({
  BTreeIndex: class BTreeIndex {
    readonly type = 'btree'
  },
  createCollection: vi.fn((options: unknown) => options),
  electricCollectionOptions: vi.fn((options: unknown) => options),
}))

vi.mock('@tanstack/db', () => ({
  BTreeIndex: adapter.BTreeIndex,
  createCollection: adapter.createCollection,
}))
vi.mock('@tanstack/electric-db-collection', () => ({
  electricCollectionOptions: adapter.electricCollectionOptions,
}))

type CapturedOptions = {
  id: string
  schema: unknown
  getKey: (row: never) => string
  syncMode: string
  autoIndex: string
  defaultIndexType: unknown
  shapeOptions: {
    url: string
    subsetMethod: string
    onError: (error: Error) => object | undefined
  }
}

afterEach(() => {
  vi.unstubAllGlobals()
  adapter.createCollection.mockClear()
  adapter.electricCollectionOptions.mockClear()
})

describe('createRouteElectricCollection', () => {
  it('rejects construction during SSR', () => {
    expect(() => createRouteElectricCollection('home', 'repositories')).toThrow('browser-only')
    expect(adapter.createCollection).not.toHaveBeenCalled()
  })

  it('uses route identity, fixed same-origin POST resources, on-demand sync, and bounded retry behavior', () => {
    vi.stubGlobal('window', { location: { origin: 'http://localhost:3000' } })
    createRouteElectricCollection('home', 'repositories')

    const options = adapter.electricCollectionOptions.mock.calls[0]?.[0] as CapturedOptions
    expect(options).toMatchObject({
      id: 'route:home:repositories',
      schema: zSyncRepository,
      syncMode: 'on-demand',
      autoIndex: 'eager',
      defaultIndexType: adapter.BTreeIndex,
      shapeOptions: { url: 'http://localhost:3000/api/v1/sync/repositories', subsetMethod: 'POST' },
    })
    expect(options.getKey({ uri: 'at://repo' } as never)).toBe('at://repo')
    expect(
      options.shapeOptions.onError(new FetchError(401, 'unauthorized', undefined, {}, '/sync')),
    ).toBeUndefined()
    expect(
      options.shapeOptions.onError(new FetchError(429, 'busy', undefined, {}, '/sync')),
    ).toEqual({})
    expect(options.shapeOptions.onError(new Error('offline'))).toEqual({})
  })
})

describe('route Electric resource mapping', () => {
  it('maps every public Sync resource to its generated schema, endpoint, and typed key', () => {
    const expected: Record<RouteElectricResource, { url: string; schema: unknown }> = {
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
    }

    for (const resource of Object.keys(expected) as RouteElectricResource[]) {
      expect(routeElectricResources[resource]).toMatchObject(expected[resource])
    }
    expect(routeElectricResources.profiles.getKey({ did: 'did:plc:alice' } as never)).toBe(
      'did:plc:alice',
    )
    expect(routeElectricResources.repositories.getKey({ uri: 'at://repo' } as never)).toBe(
      'at://repo',
    )
    expect(routeElectricResources.stars.getKey({ uri: 'at://star' } as never)).toBe('at://star')
    expect(routeElectricResources.issues.getKey({ uri: 'at://issue' } as never)).toBe('at://issue')
    expect(routeElectricResources['issue-comments'].getKey({ uri: 'at://comment' } as never)).toBe(
      'at://comment',
    )
    expect(routeElectricResources['pull-requests'].getKey({ uri: 'at://pr' } as never)).toBe(
      'at://pr',
    )
    expect(
      routeElectricResources['pull-request-reviews'].getKey({ uri: 'at://review' } as never),
    ).toBe('at://review')
  })
})
